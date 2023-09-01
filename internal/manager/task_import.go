package manager

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/jsonschema"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/movie"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stashapp/stash/pkg/studio"
	"github.com/stashapp/stash/pkg/tag"
)

type ImportTask struct {
	txnManager Repository
	json       jsonUtils

	BaseDir             string
	TmpZip              string
	Reset               bool
	DuplicateBehaviour  ImportDuplicateEnum
	MissingRefBehaviour models.ImportMissingRefEnum

	fileNamingAlgorithm models.HashAlgorithm
}

type ImportObjectsInput struct {
	File                graphql.Upload              `json:"file"`
	DuplicateBehaviour  ImportDuplicateEnum         `json:"duplicateBehaviour"`
	MissingRefBehaviour models.ImportMissingRefEnum `json:"missingRefBehaviour"`
}

func CreateImportTask(a models.HashAlgorithm, input ImportObjectsInput) (*ImportTask, error) {
	baseDir, err := instance.Paths.Generated.TempDir("import")
	if err != nil {
		logger.Errorf("error creating temporary directory for import: %s", err.Error())
		return nil, err
	}

	tmpZip := ""
	if input.File.File != nil {
		tmpZip = filepath.Join(baseDir, "import.zip")
		out, err := os.Create(tmpZip)
		if err != nil {
			return nil, err
		}

		_, err = io.Copy(out, input.File.File)
		out.Close()
		if err != nil {
			return nil, err
		}
	}

	return &ImportTask{
		txnManager:          GetInstance().Repository,
		BaseDir:             baseDir,
		TmpZip:              tmpZip,
		Reset:               false,
		DuplicateBehaviour:  input.DuplicateBehaviour,
		MissingRefBehaviour: input.MissingRefBehaviour,
		fileNamingAlgorithm: a,
	}, nil
}

func (t *ImportTask) GetDescription() string {
	return "Importing..."
}

func (t *ImportTask) Start(ctx context.Context) {
	if t.TmpZip != "" {
		defer func() {
			err := fsutil.RemoveDir(t.BaseDir)
			if err != nil {
				logger.Errorf("error removing directory %s: %s", t.BaseDir, err.Error())
			}
		}()

		if err := t.unzipFile(); err != nil {
			logger.Errorf("error unzipping provided file for import: %s", err.Error())
			return
		}
	}

	t.json = jsonUtils{
		json: *paths.GetJSONPaths(t.BaseDir),
	}

	// set default behaviour if not provided
	if !t.DuplicateBehaviour.IsValid() {
		t.DuplicateBehaviour = ImportDuplicateEnumFail
	}
	if !t.MissingRefBehaviour.IsValid() {
		t.MissingRefBehaviour = models.ImportMissingRefEnumFail
	}

	if t.Reset {
		err := t.txnManager.Reset()

		if err != nil {
			logger.Errorf("Error resetting database: %s", err.Error())
			return
		}
	}

	t.ImportTags(ctx)
	t.ImportPerformers(ctx)
	t.ImportStudios(ctx)
	t.ImportMovies(ctx)
	t.ImportFiles(ctx)
	t.ImportGalleries(ctx)

	t.ImportScenes(ctx)
	t.ImportImages(ctx)
}

func (t *ImportTask) unzipFile() error {
	defer func() {
		err := os.Remove(t.TmpZip)
		if err != nil {
			logger.Errorf("error removing temporary zip file %s: %s", t.TmpZip, err.Error())
		}
	}()

	// now we can read the zip file
	r, err := zip.OpenReader(t.TmpZip)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fn := filepath.Join(t.BaseDir, f.Name)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fn, os.ModePerm); err != nil {
				logger.Warnf("couldn't create directory %v while unzipping import file: %v", fn, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fn), os.ModePerm); err != nil {
			return err
		}

		o, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		i, err := f.Open()
		if err != nil {
			o.Close()
			return err
		}

		if _, err := io.Copy(o, i); err != nil {
			o.Close()
			i.Close()
			return err
		}

		o.Close()
		i.Close()
	}

	return nil
}

func (t *ImportTask) ImportPerformers(ctx context.Context) {
	logger.Info("[performers] importing")

	path := t.json.json.Performers
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[performers] failed to read performers directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1
		performerJSON, err := jsonschema.LoadPerformerFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Errorf("[performers] failed to read json: %s", err.Error())
			continue
		}

		logger.Progressf("[performers] %d of %d", index, len(files))

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			r := t.txnManager
			readerWriter := r.Performer
			importer := &performer.Importer{
				ReaderWriter: readerWriter,
				TagWriter:    r.Tag,
				Input:        *performerJSON,
			}

			return performImport(ctx, importer, t.DuplicateBehaviour)
		}); err != nil {
			logger.Errorf("[performers] <%s> import failed: %s", fi.Name(), err.Error())
		}
	}

	logger.Info("[performers] import complete")
}

func (t *ImportTask) ImportStudios(ctx context.Context) {
	pendingParent := make(map[string][]*jsonschema.Studio)

	logger.Info("[studios] importing")

	path := t.json.json.Studios
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[studios] failed to read studios directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1
		studioJSON, err := jsonschema.LoadStudioFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Errorf("[studios] failed to read json: %s", err.Error())
			continue
		}

		logger.Progressf("[studios] %d of %d", index, len(files))

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			return t.ImportStudio(ctx, studioJSON, pendingParent, t.txnManager.Studio)
		}); err != nil {
			if errors.Is(err, studio.ErrParentStudioNotExist) {
				// add to the pending parent list so that it is created after the parent
				s := pendingParent[studioJSON.ParentStudio]
				s = append(s, studioJSON)
				pendingParent[studioJSON.ParentStudio] = s
				continue
			}

			logger.Errorf("[studios] <%s> failed to create: %s", fi.Name(), err.Error())
			continue
		}
	}

	// create the leftover studios, warning for missing parents
	if len(pendingParent) > 0 {
		logger.Warnf("[studios] importing studios with missing parents")

		for _, s := range pendingParent {
			for _, orphanStudioJSON := range s {
				if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
					return t.ImportStudio(ctx, orphanStudioJSON, nil, t.txnManager.Studio)
				}); err != nil {
					logger.Errorf("[studios] <%s> failed to create: %s", orphanStudioJSON.Name, err.Error())
					continue
				}
			}
		}
	}

	logger.Info("[studios] import complete")
}

func (t *ImportTask) ImportStudio(ctx context.Context, studioJSON *jsonschema.Studio, pendingParent map[string][]*jsonschema.Studio, readerWriter studio.ImporterReaderWriter) error {
	importer := &studio.Importer{
		ReaderWriter:        readerWriter,
		Input:               *studioJSON,
		MissingRefBehaviour: t.MissingRefBehaviour,
	}

	// first phase: return error if parent does not exist
	if pendingParent != nil {
		importer.MissingRefBehaviour = models.ImportMissingRefEnumFail
	}

	if err := performImport(ctx, importer, t.DuplicateBehaviour); err != nil {
		return err
	}

	// now create the studios pending this studios creation
	s := pendingParent[studioJSON.Name]
	for _, childStudioJSON := range s {
		// map is nil since we're not checking parent studios at this point
		if err := t.ImportStudio(ctx, childStudioJSON, nil, readerWriter); err != nil {
			return fmt.Errorf("failed to create child studio <%s>: %s", childStudioJSON.Name, err.Error())
		}
	}

	// delete the entry from the map so that we know its not left over
	delete(pendingParent, studioJSON.Name)

	return nil
}

func (t *ImportTask) ImportMovies(ctx context.Context) {
	logger.Info("[movies] importing")

	path := t.json.json.Movies
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[movies] failed to read movies directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1
		movieJSON, err := jsonschema.LoadMovieFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Errorf("[movies] failed to read json: %s", err.Error())
			continue
		}

		logger.Progressf("[movies] %d of %d", index, len(files))

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			r := t.txnManager
			readerWriter := r.Movie
			studioReaderWriter := r.Studio

			movieImporter := &movie.Importer{
				ReaderWriter:        readerWriter,
				StudioWriter:        studioReaderWriter,
				Input:               *movieJSON,
				MissingRefBehaviour: t.MissingRefBehaviour,
			}

			return performImport(ctx, movieImporter, t.DuplicateBehaviour)
		}); err != nil {
			logger.Errorf("[movies] <%s> import failed: %s", fi.Name(), err.Error())
			continue
		}
	}

	logger.Info("[movies] import complete")
}

func (t *ImportTask) ImportFiles(ctx context.Context) {
	logger.Info("[files] importing")

	path := t.json.json.Files
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[files] failed to read files directory: %v", err)
		}

		return
	}

	pendingParent := make(map[string][]jsonschema.DirEntry)

	for i, fi := range files {
		index := i + 1
		fileJSON, err := jsonschema.LoadFileFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Errorf("[files] failed to read json: %s", err.Error())
			continue
		}

		logger.Progressf("[files] %d of %d", index, len(files))

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			return t.ImportFile(ctx, fileJSON, pendingParent)
		}); err != nil {
			if errors.Is(err, file.ErrZipFileNotExist) {
				// add to the pending parent list so that it is created after the parent
				s := pendingParent[fileJSON.DirEntry().ZipFile]
				s = append(s, fileJSON)
				pendingParent[fileJSON.DirEntry().ZipFile] = s
				continue
			}

			logger.Errorf("[files] <%s> failed to create: %s", fi.Name(), err.Error())
			continue
		}
	}

	// create the leftover studios, warning for missing parents
	if len(pendingParent) > 0 {
		logger.Warnf("[files] importing files with missing zip files")

		for _, s := range pendingParent {
			for _, orphanFileJSON := range s {
				if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
					return t.ImportFile(ctx, orphanFileJSON, nil)
				}); err != nil {
					logger.Errorf("[files] <%s> failed to create: %s", orphanFileJSON.DirEntry().Path, err.Error())
					continue
				}
			}
		}
	}

	logger.Info("[files] import complete")
}

func (t *ImportTask) ImportFile(ctx context.Context, fileJSON jsonschema.DirEntry, pendingParent map[string][]jsonschema.DirEntry) error {
	r := t.txnManager
	readerWriter := r.File

	fileImporter := &file.Importer{
		ReaderWriter: readerWriter,
		FolderStore:  r.Folder,
		Input:        fileJSON,
	}

	// ignore duplicate files - don't overwrite
	if err := performImport(ctx, fileImporter, ImportDuplicateEnumIgnore); err != nil {
		return err
	}

	// now create the files pending this file's creation
	s := pendingParent[fileJSON.DirEntry().Path]
	for _, childFileJSON := range s {
		// map is nil since we're not checking parent studios at this point
		if err := t.ImportFile(ctx, childFileJSON, nil); err != nil {
			return fmt.Errorf("failed to create child file <%s>: %s", childFileJSON.DirEntry().Path, err.Error())
		}
	}

	// delete the entry from the map so that we know its not left over
	delete(pendingParent, fileJSON.DirEntry().Path)

	return nil
}

func (t *ImportTask) ImportGalleries(ctx context.Context) {
	logger.Info("[galleries] importing")

	path := t.json.json.Galleries
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[galleries] failed to read galleries directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1
		galleryJSON, err := jsonschema.LoadGalleryFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Errorf("[galleries] failed to read json: %s", err.Error())
			continue
		}

		logger.Progressf("[galleries] %d of %d", index, len(files))

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			r := t.txnManager
			readerWriter := r.Gallery
			tagWriter := r.Tag
			performerWriter := r.Performer
			studioWriter := r.Studio
			chapterWriter := r.GalleryChapter

			galleryImporter := &gallery.Importer{
				ReaderWriter:        readerWriter,
				FolderFinder:        r.Folder,
				FileFinder:          r.File,
				PerformerWriter:     performerWriter,
				StudioWriter:        studioWriter,
				TagWriter:           tagWriter,
				Input:               *galleryJSON,
				MissingRefBehaviour: t.MissingRefBehaviour,
			}

			if err := performImport(ctx, galleryImporter, t.DuplicateBehaviour); err != nil {
				return err
			}

			// import the gallery chapters
			for _, m := range galleryJSON.Chapters {
				chapterImporter := &gallery.ChapterImporter{
					GalleryID:           galleryImporter.ID,
					Input:               m,
					MissingRefBehaviour: t.MissingRefBehaviour,
					ReaderWriter:        chapterWriter,
				}

				if err := performImport(ctx, chapterImporter, t.DuplicateBehaviour); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			logger.Errorf("[galleries] <%s> import failed to commit: %s", fi.Name(), err.Error())
			continue
		}
	}

	logger.Info("[galleries] import complete")
}

func (t *ImportTask) ImportTags(ctx context.Context) {
	pendingParent := make(map[string][]*jsonschema.Tag)
	logger.Info("[tags] importing")

	path := t.json.json.Tags
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[tags] failed to read tags directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1
		tagJSON, err := jsonschema.LoadTagFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Errorf("[tags] failed to read json: %s", err.Error())
			continue
		}

		logger.Progressf("[tags] %d of %d", index, len(files))

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			return t.ImportTag(ctx, tagJSON, pendingParent, false, t.txnManager.Tag)
		}); err != nil {
			var parentError tag.ParentTagNotExistError
			if errors.As(err, &parentError) {
				pendingParent[parentError.MissingParent()] = append(pendingParent[parentError.MissingParent()], tagJSON)
				continue
			}

			logger.Errorf("[tags] <%s> failed to import: %s", fi.Name(), err.Error())
			continue
		}
	}

	for _, s := range pendingParent {
		for _, orphanTagJSON := range s {
			if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
				return t.ImportTag(ctx, orphanTagJSON, nil, true, t.txnManager.Tag)
			}); err != nil {
				logger.Errorf("[tags] <%s> failed to create: %s", orphanTagJSON.Name, err.Error())
				continue
			}
		}
	}

	logger.Info("[tags] import complete")
}

func (t *ImportTask) ImportTag(ctx context.Context, tagJSON *jsonschema.Tag, pendingParent map[string][]*jsonschema.Tag, fail bool, readerWriter tag.ImporterReaderWriter) error {
	importer := &tag.Importer{
		ReaderWriter:        readerWriter,
		Input:               *tagJSON,
		MissingRefBehaviour: t.MissingRefBehaviour,
	}

	// first phase: return error if parent does not exist
	if !fail {
		importer.MissingRefBehaviour = models.ImportMissingRefEnumFail
	}

	if err := performImport(ctx, importer, t.DuplicateBehaviour); err != nil {
		return err
	}

	for _, childTagJSON := range pendingParent[tagJSON.Name] {
		if err := t.ImportTag(ctx, childTagJSON, pendingParent, fail, readerWriter); err != nil {
			var parentError tag.ParentTagNotExistError
			if errors.As(err, &parentError) {
				pendingParent[parentError.MissingParent()] = append(pendingParent[parentError.MissingParent()], childTagJSON)
				continue
			}

			return fmt.Errorf("failed to create child tag <%s>: %s", childTagJSON.Name, err.Error())
		}
	}

	delete(pendingParent, tagJSON.Name)

	return nil
}

func (t *ImportTask) ImportScenes(ctx context.Context) {
	logger.Info("[scenes] importing")

	path := t.json.json.Scenes
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[scenes] failed to read scenes directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1

		logger.Progressf("[scenes] %d of %d", index, len(files))

		sceneJSON, err := jsonschema.LoadSceneFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Infof("[scenes] <%s> json parse failure: %s", fi.Name(), err.Error())
			continue
		}

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			r := t.txnManager
			readerWriter := r.Scene
			tagWriter := r.Tag
			galleryWriter := r.Gallery
			movieWriter := r.Movie
			performerWriter := r.Performer
			studioWriter := r.Studio
			markerWriter := r.SceneMarker

			sceneImporter := &scene.Importer{
				ReaderWriter: readerWriter,
				Input:        *sceneJSON,
				FileFinder:   r.File,

				FileNamingAlgorithm: t.fileNamingAlgorithm,
				MissingRefBehaviour: t.MissingRefBehaviour,

				GalleryFinder:   galleryWriter,
				MovieWriter:     movieWriter,
				PerformerWriter: performerWriter,
				StudioWriter:    studioWriter,
				TagWriter:       tagWriter,
			}

			if err := performImport(ctx, sceneImporter, t.DuplicateBehaviour); err != nil {
				return err
			}

			// import the scene markers
			for _, m := range sceneJSON.Markers {
				markerImporter := &scene.MarkerImporter{
					SceneID:             sceneImporter.ID,
					Input:               m,
					MissingRefBehaviour: t.MissingRefBehaviour,
					ReaderWriter:        markerWriter,
					TagWriter:           tagWriter,
				}

				if err := performImport(ctx, markerImporter, t.DuplicateBehaviour); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			logger.Errorf("[scenes] <%s> import failed: %s", fi.Name(), err.Error())
		}
	}

	logger.Info("[scenes] import complete")
}

func (t *ImportTask) ImportImages(ctx context.Context) {
	logger.Info("[images] importing")

	path := t.json.json.Images
	files, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Errorf("[images] failed to read images directory: %v", err)
		}

		return
	}

	for i, fi := range files {
		index := i + 1

		logger.Progressf("[images] %d of %d", index, len(files))

		imageJSON, err := jsonschema.LoadImageFile(filepath.Join(path, fi.Name()))
		if err != nil {
			logger.Infof("[images] <%s> json parse failure: %s", fi.Name(), err.Error())
			continue
		}

		if err := t.txnManager.WithTxn(ctx, func(ctx context.Context) error {
			r := t.txnManager
			readerWriter := r.Image
			tagWriter := r.Tag
			galleryWriter := r.Gallery
			performerWriter := r.Performer
			studioWriter := r.Studio

			imageImporter := &image.Importer{
				ReaderWriter: readerWriter,
				FileFinder:   r.File,
				Input:        *imageJSON,

				MissingRefBehaviour: t.MissingRefBehaviour,

				GalleryFinder:   galleryWriter,
				PerformerWriter: performerWriter,
				StudioWriter:    studioWriter,
				TagWriter:       tagWriter,
			}

			return performImport(ctx, imageImporter, t.DuplicateBehaviour)
		}); err != nil {
			logger.Errorf("[images] <%s> import failed: %s", fi.Name(), err.Error())
		}
	}

	logger.Info("[images] import complete")
}
