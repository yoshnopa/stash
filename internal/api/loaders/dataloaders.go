//go:generate go run github.com/vektah/dataloaden SceneLoader int *github.com/stashapp/stash/pkg/models.Scene
//go:generate go run github.com/vektah/dataloaden GalleryLoader int *github.com/stashapp/stash/pkg/models.Gallery
//go:generate go run github.com/vektah/dataloaden ImageLoader int *github.com/stashapp/stash/pkg/models.Image
//go:generate go run github.com/vektah/dataloaden PerformerLoader int *github.com/stashapp/stash/pkg/models.Performer
//go:generate go run github.com/vektah/dataloaden StudioLoader int *github.com/stashapp/stash/pkg/models.Studio
//go:generate go run github.com/vektah/dataloaden TagLoader int *github.com/stashapp/stash/pkg/models.Tag
//go:generate go run github.com/vektah/dataloaden MovieLoader int *github.com/stashapp/stash/pkg/models.Movie
//go:generate go run github.com/vektah/dataloaden FileLoader github.com/stashapp/stash/pkg/models.FileID github.com/stashapp/stash/pkg/models.File
//go:generate go run github.com/vektah/dataloaden SceneFileIDsLoader int []github.com/stashapp/stash/pkg/models.FileID
//go:generate go run github.com/vektah/dataloaden ImageFileIDsLoader int []github.com/stashapp/stash/pkg/models.FileID
//go:generate go run github.com/vektah/dataloaden GalleryFileIDsLoader int []github.com/stashapp/stash/pkg/models.FileID

package loaders

import (
	"context"
	"net/http"
	"time"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type contextKey struct{ name string }

var (
	loadersCtxKey = &contextKey{"loaders"}
)

const (
	wait     = 1 * time.Millisecond
	maxBatch = 100
)

type Loaders struct {
	SceneByID    *SceneLoader
	SceneFiles   *SceneFileIDsLoader
	ImageFiles   *ImageFileIDsLoader
	GalleryFiles *GalleryFileIDsLoader

	GalleryByID   *GalleryLoader
	ImageByID     *ImageLoader
	PerformerByID *PerformerLoader
	StudioByID    *StudioLoader
	TagByID       *TagLoader
	MovieByID     *MovieLoader
	FileByID      *FileLoader
}

type Middleware struct {
	DatabaseProvider txn.DatabaseProvider
	Repository       manager.Repository
}

func (m Middleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ldrs := Loaders{
			SceneByID: &SceneLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchScenes(ctx),
			},
			GalleryByID: &GalleryLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchGalleries(ctx),
			},
			ImageByID: &ImageLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchImages(ctx),
			},
			PerformerByID: &PerformerLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchPerformers(ctx),
			},
			StudioByID: &StudioLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchStudios(ctx),
			},
			TagByID: &TagLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchTags(ctx),
			},
			MovieByID: &MovieLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchMovies(ctx),
			},
			FileByID: &FileLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchFiles(ctx),
			},
			SceneFiles: &SceneFileIDsLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchScenesFileIDs(ctx),
			},
			ImageFiles: &ImageFileIDsLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchImagesFileIDs(ctx),
			},
			GalleryFiles: &GalleryFileIDsLoader{
				wait:     wait,
				maxBatch: maxBatch,
				fetch:    m.fetchGalleriesFileIDs(ctx),
			},
		}

		newCtx := context.WithValue(r.Context(), loadersCtxKey, ldrs)
		next.ServeHTTP(w, r.WithContext(newCtx))
	})
}

func From(ctx context.Context) Loaders {
	return ctx.Value(loadersCtxKey).(Loaders)
}

func toErrorSlice(err error) []error {
	if err != nil {
		return []error{err}
	}

	return nil
}

func (m Middleware) withTxn(ctx context.Context, fn func(ctx context.Context) error) error {
	return txn.WithDatabase(ctx, m.DatabaseProvider, fn)
}

func (m Middleware) fetchScenes(ctx context.Context) func(keys []int) ([]*models.Scene, []error) {
	return func(keys []int) (ret []*models.Scene, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Scene.FindMany(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchImages(ctx context.Context) func(keys []int) ([]*models.Image, []error) {
	return func(keys []int) (ret []*models.Image, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Image.FindMany(ctx, keys)
			return err
		})

		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchGalleries(ctx context.Context) func(keys []int) ([]*models.Gallery, []error) {
	return func(keys []int) (ret []*models.Gallery, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Gallery.FindMany(ctx, keys)
			return err
		})

		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchPerformers(ctx context.Context) func(keys []int) ([]*models.Performer, []error) {
	return func(keys []int) (ret []*models.Performer, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Performer.FindMany(ctx, keys)
			return err
		})

		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchStudios(ctx context.Context) func(keys []int) ([]*models.Studio, []error) {
	return func(keys []int) (ret []*models.Studio, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Studio.FindMany(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchTags(ctx context.Context) func(keys []int) ([]*models.Tag, []error) {
	return func(keys []int) (ret []*models.Tag, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Tag.FindMany(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchMovies(ctx context.Context) func(keys []int) ([]*models.Movie, []error) {
	return func(keys []int) (ret []*models.Movie, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Movie.FindMany(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchFiles(ctx context.Context) func(keys []models.FileID) ([]models.File, []error) {
	return func(keys []models.FileID) (ret []models.File, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.File.Find(ctx, keys...)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchScenesFileIDs(ctx context.Context) func(keys []int) ([][]models.FileID, []error) {
	return func(keys []int) (ret [][]models.FileID, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Scene.GetManyFileIDs(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchImagesFileIDs(ctx context.Context) func(keys []int) ([][]models.FileID, []error) {
	return func(keys []int) (ret [][]models.FileID, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Image.GetManyFileIDs(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}

func (m Middleware) fetchGalleriesFileIDs(ctx context.Context) func(keys []int) ([][]models.FileID, []error) {
	return func(keys []int) (ret [][]models.FileID, errs []error) {
		err := m.withTxn(ctx, func(ctx context.Context) error {
			var err error
			ret, err = m.Repository.Gallery.GetManyFileIDs(ctx, keys)
			return err
		})
		return ret, toErrorSlice(err)
	}
}
