package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stashapp/stash/internal/desktop"
	"github.com/stashapp/stash/internal/dlna"
	"github.com/stashapp/stash/internal/log"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/file"
	file_image "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stashapp/stash/pkg/scraper"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/utils"
	"github.com/stashapp/stash/ui"

	// register custom migrations
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
)

type SystemStatus struct {
	DatabaseSchema *int             `json:"databaseSchema"`
	DatabasePath   *string          `json:"databasePath"`
	ConfigPath     *string          `json:"configPath"`
	AppSchema      int              `json:"appSchema"`
	Status         SystemStatusEnum `json:"status"`
}

type SystemStatusEnum string

const (
	SystemStatusEnumSetup          SystemStatusEnum = "SETUP"
	SystemStatusEnumNeedsMigration SystemStatusEnum = "NEEDS_MIGRATION"
	SystemStatusEnumOk             SystemStatusEnum = "OK"
)

var AllSystemStatusEnum = []SystemStatusEnum{
	SystemStatusEnumSetup,
	SystemStatusEnumNeedsMigration,
	SystemStatusEnumOk,
}

func (e SystemStatusEnum) IsValid() bool {
	switch e {
	case SystemStatusEnumSetup, SystemStatusEnumNeedsMigration, SystemStatusEnumOk:
		return true
	}
	return false
}

func (e SystemStatusEnum) String() string {
	return string(e)
}

func (e *SystemStatusEnum) UnmarshalGQL(v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("enums must be strings")
	}

	*e = SystemStatusEnum(str)
	if !e.IsValid() {
		return fmt.Errorf("%s is not a valid SystemStatusEnum", str)
	}
	return nil
}

func (e SystemStatusEnum) MarshalGQL(w io.Writer) {
	fmt.Fprint(w, strconv.Quote(e.String()))
}

type SetupInput struct {
	// Empty to indicate $HOME/.stash/config.yml default
	ConfigLocation string                     `json:"configLocation"`
	Stashes        []*config.StashConfigInput `json:"stashes"`
	// Empty to indicate default
	DatabaseFile string `json:"databaseFile"`
	// Empty to indicate default
	GeneratedLocation string `json:"generatedLocation"`
	// Empty to indicate default
	CacheLocation string `json:"cacheLocation"`

	StoreBlobsInDatabase bool `json:"storeBlobsInDatabase"`
	// Empty to indicate default
	BlobsLocation string `json:"blobsLocation"`
}

type Manager struct {
	Config *config.Instance
	Logger *log.Logger

	Paths *paths.Paths

	FFMPEG        *ffmpeg.FFMpeg
	FFProbe       ffmpeg.FFProbe
	StreamManager *ffmpeg.StreamManager

	ReadLockManager *fsutil.ReadLockManager

	SessionStore *session.Store

	JobManager *job.Manager

	PluginCache  *plugin.Cache
	ScraperCache *scraper.Cache

	DownloadStore *DownloadStore

	DLNAService *dlna.Service

	Database   *sqlite.Database
	Repository Repository

	SceneService   SceneService
	ImageService   ImageService
	GalleryService GalleryService

	Scanner *file.Scanner
	Cleaner *file.Cleaner

	scanSubs *subscriptionManager
}

var instance *Manager
var once sync.Once

func GetInstance() *Manager {
	if _, err := Initialize(); err != nil {
		panic(err)
	}
	return instance
}

func Initialize() (*Manager, error) {
	var err error
	once.Do(func() {
		err = initialize()
	})

	return instance, err
}

func initialize() error {
	ctx := context.TODO()
	cfg, err := config.Initialize()

	if err != nil {
		return fmt.Errorf("initializing configuration: %w", err)
	}

	l := initLog()
	initProfiling(cfg.GetCPUProfilePath())

	db := sqlite.NewDatabase()

	// start with empty paths
	emptyPaths := paths.Paths{}

	instance = &Manager{
		Config:          cfg,
		Logger:          l,
		ReadLockManager: fsutil.NewReadLockManager(),
		DownloadStore:   NewDownloadStore(),
		PluginCache:     plugin.NewCache(cfg),

		Database:   db,
		Repository: sqliteRepository(db),
		Paths:      &emptyPaths,

		scanSubs: &subscriptionManager{},
	}

	instance.SceneService = &scene.Service{
		File:             db.File,
		Repository:       db.Scene,
		MarkerRepository: db.SceneMarker,
		PluginCache:      instance.PluginCache,
		Paths:            instance.Paths,
		Config:           cfg,
	}

	instance.ImageService = &image.Service{
		File:       db.File,
		Repository: db.Image,
	}

	instance.GalleryService = &gallery.Service{
		Repository:   db.Gallery,
		ImageFinder:  db.Image,
		ImageService: instance.ImageService,
		File:         db.File,
		Folder:       db.Folder,
	}

	instance.JobManager = initJobManager()

	sceneServer := SceneServer{
		TxnManager:       instance.Repository,
		SceneCoverGetter: instance.Repository.Scene,
	}

	instance.DLNAService = dlna.NewService(instance.Repository, dlna.Repository{
		SceneFinder:     instance.Repository.Scene,
		FileGetter:      instance.Repository.File,
		StudioFinder:    instance.Repository.Studio,
		TagFinder:       instance.Repository.Tag,
		PerformerFinder: instance.Repository.Performer,
		MovieFinder:     instance.Repository.Movie,
	}, instance.Config, &sceneServer)

	if !cfg.IsNewSystem() {
		logger.Infof("using config file: %s", cfg.GetConfigFile())

		if err == nil {
			err = cfg.Validate()
		}

		if err != nil {
			return fmt.Errorf("error initializing configuration: %w", err)
		}

		if err := instance.PostInit(ctx); err != nil {
			var migrationNeededErr *sqlite.MigrationNeededError
			if errors.As(err, &migrationNeededErr) {
				logger.Warn(err.Error())
			} else {
				return err
			}
		}

		initSecurity(cfg)
	} else {
		cfgFile := cfg.GetConfigFile()
		if cfgFile != "" {
			cfgFile += " "
		}

		// create temporary session store - this will be re-initialised
		// after config is complete
		instance.SessionStore = session.NewStore(cfg)

		logger.Warnf("config file %snot found. Assuming new system...", cfgFile)
	}

	if err = initFFMPEG(ctx); err != nil {
		logger.Warnf("could not initialize FFMPEG subsystem: %v", err)
	}

	instance.Scanner = makeScanner(db, instance.PluginCache)
	instance.Cleaner = makeCleaner(db, instance.PluginCache)

	// if DLNA is enabled, start it now
	if instance.Config.GetDLNADefaultEnabled() {
		if err := instance.DLNAService.Start(nil); err != nil {
			logger.Warnf("could not start DLNA service: %v", err)
		}
	}

	return nil
}

func videoFileFilter(ctx context.Context, f models.File) bool {
	return useAsVideo(f.Base().Path)
}

func imageFileFilter(ctx context.Context, f models.File) bool {
	return useAsImage(f.Base().Path)
}

func galleryFileFilter(ctx context.Context, f models.File) bool {
	return isZip(f.Base().Basename)
}

func makeScanner(db *sqlite.Database, pluginCache *plugin.Cache) *file.Scanner {
	return &file.Scanner{
		Repository: file.Repository{
			Manager:          db,
			DatabaseProvider: db,
			FileStore:        db.File,
			FolderStore:      db.Folder,
		},
		FileDecorators: []file.Decorator{
			&file.FilteredDecorator{
				Decorator: &video.Decorator{
					FFProbe: instance.FFProbe,
				},
				Filter: file.FilterFunc(videoFileFilter),
			},
			&file.FilteredDecorator{
				Decorator: &file_image.Decorator{
					FFProbe: instance.FFProbe,
				},
				Filter: file.FilterFunc(imageFileFilter),
			},
		},
		FingerprintCalculator: &fingerprintCalculator{instance.Config},
		FS:                    &file.OsFS{},
	}
}

func makeCleaner(db *sqlite.Database, pluginCache *plugin.Cache) *file.Cleaner {
	return &file.Cleaner{
		FS: &file.OsFS{},
		Repository: file.Repository{
			Manager:          db,
			DatabaseProvider: db,
			FileStore:        db.File,
			FolderStore:      db.Folder,
		},
		Handlers: []file.CleanHandler{
			&cleanHandler{},
		},
	}
}

func initJobManager() *job.Manager {
	ret := job.NewManager()

	// desktop notifications
	ctx := context.Background()
	c := ret.Subscribe(context.Background())
	go func() {
		for {
			select {
			case j := <-c.RemovedJob:
				if instance.Config.GetNotificationsEnabled() {
					cleanDesc := strings.TrimRight(j.Description, ".")

					if j.StartTime == nil {
						// Task was never started
						return
					}

					timeElapsed := j.EndTime.Sub(*j.StartTime)
					desktop.SendNotification("Task Finished", "Task \""+cleanDesc+"\" is finished in "+formatDuration(timeElapsed)+".")
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return ret
}

func formatDuration(t time.Duration) string {
	return fmt.Sprintf("%02.f:%02.f:%02.f", t.Hours(), t.Minutes(), t.Seconds())
}

func initSecurity(cfg *config.Instance) {
	if err := session.CheckExternalAccessTripwire(cfg); err != nil {
		session.LogExternalAccessError(*err)
	}
}

func initProfiling(cpuProfilePath string) {
	if cpuProfilePath == "" {
		return
	}

	f, err := os.Create(cpuProfilePath)
	if err != nil {
		logger.Fatalf("unable to create cpu profile file: %s", err.Error())
	}

	logger.Infof("profiling to %s", cpuProfilePath)

	// StopCPUProfile is defer called in main
	if err = pprof.StartCPUProfile(f); err != nil {
		logger.Warnf("could not start CPU profiling: %v", err)
	}
}

func initFFMPEG(ctx context.Context) error {
	// only do this if we have a config file set
	if instance.Config.GetConfigFile() != "" {
		// use same directory as config path
		configDirectory := instance.Config.GetConfigPath()
		paths := []string{
			configDirectory,
			paths.GetStashHomeDirectory(),
		}
		ffmpegPath, ffprobePath := ffmpeg.GetPaths(paths)

		if ffmpegPath == "" || ffprobePath == "" {
			logger.Infof("couldn't find FFMPEG, attempting to download it")
			if err := ffmpeg.Download(ctx, configDirectory); err != nil {
				msg := `Unable to locate / automatically download FFMPEG

	Check the readme for download links.
	The FFMPEG and FFProbe binaries should be placed in %s

	The error was: %s
	`
				logger.Errorf(msg, configDirectory, err)
				return err
			} else {
				// After download get new paths for ffmpeg and ffprobe
				ffmpegPath, ffprobePath = ffmpeg.GetPaths(paths)
			}
		}

		instance.FFMPEG = ffmpeg.NewEncoder(ffmpegPath)
		instance.FFProbe = ffmpeg.FFProbe(ffprobePath)

		instance.FFMPEG.InitHWSupport(ctx)
		instance.RefreshStreamManager()
	}

	return nil
}

func initLog() *log.Logger {
	config := config.GetInstance()
	l := log.NewLogger()
	l.Init(config.GetLogFile(), config.GetLogOut(), config.GetLogLevel())
	logger.Logger = l

	return l
}

// PostInit initialises the paths, caches and txnManager after the initial
// configuration has been set. Should only be called if the configuration
// is valid.
func (s *Manager) PostInit(ctx context.Context) error {
	if err := s.Config.SetInitialConfig(); err != nil {
		logger.Warnf("could not set initial configuration: %v", err)
	}

	*s.Paths = paths.NewPaths(s.Config.GetGeneratedPath(), s.Config.GetBlobsPath())
	s.RefreshConfig()
	s.SessionStore = session.NewStore(s.Config)
	s.PluginCache.RegisterSessionStore(s.SessionStore)

	if err := s.PluginCache.LoadPlugins(); err != nil {
		logger.Errorf("Error reading plugin configs: %s", err.Error())
	}

	s.SetBlobStoreOptions()

	s.ScraperCache = instance.initScraperCache()
	writeStashIcon()

	// clear the downloads and tmp directories
	// #1021 - only clear these directories if the generated folder is non-empty
	if s.Config.GetGeneratedPath() != "" {
		const deleteTimeout = 1 * time.Second

		utils.Timeout(func() {
			if err := fsutil.EmptyDir(instance.Paths.Generated.Downloads); err != nil {
				logger.Warnf("could not empty Downloads directory: %v", err)
			}
			if err := fsutil.EnsureDir(instance.Paths.Generated.Tmp); err != nil {
				logger.Warnf("could not create Tmp directory: %v", err)
			} else {
				if err := fsutil.EmptyDir(instance.Paths.Generated.Tmp); err != nil {
					logger.Warnf("could not empty Tmp directory: %v", err)
				}
			}
		}, deleteTimeout, func(done chan struct{}) {
			logger.Info("Please wait. Deleting temporary files...") // print
			<-done                                                  // and wait for deletion
			logger.Info("Temporary files deleted.")
		})
	}

	database := s.Database
	if err := database.Open(s.Config.GetDatabasePath()); err != nil {
		return err
	}

	// Set the proxy if defined in config
	if s.Config.GetProxy() != "" {
		os.Setenv("HTTP_PROXY", s.Config.GetProxy())
		os.Setenv("HTTPS_PROXY", s.Config.GetProxy())
		os.Setenv("NO_PROXY", s.Config.GetNoProxy())
		logger.Info("Using HTTP Proxy")
	}

	return nil
}

func (s *Manager) SetBlobStoreOptions() {
	storageType := s.Config.GetBlobsStorage()
	blobsPath := s.Config.GetBlobsPath()

	s.Database.SetBlobStoreOptions(sqlite.BlobStoreOptions{
		UseFilesystem: storageType == config.BlobStorageTypeFilesystem,
		UseDatabase:   storageType == config.BlobStorageTypeDatabase,
		Path:          blobsPath,
	})
}

func writeStashIcon() {
	iconPath := filepath.Join(instance.Config.GetConfigPath(), "icon.png")
	err := os.WriteFile(iconPath, ui.FaviconProvider.GetFaviconPng(), 0644)
	if err != nil {
		logger.Errorf("Couldn't write icon file: %s", err.Error())
	}
}

// initScraperCache initializes a new scraper cache and returns it.
func (s *Manager) initScraperCache() *scraper.Cache {
	ret, err := scraper.NewCache(config.GetInstance(), s.Repository, scraper.Repository{
		SceneFinder:     s.Repository.Scene,
		GalleryFinder:   s.Repository.Gallery,
		TagFinder:       s.Repository.Tag,
		PerformerFinder: s.Repository.Performer,
		MovieFinder:     s.Repository.Movie,
		StudioFinder:    s.Repository.Studio,
	})

	if err != nil {
		logger.Errorf("Error reading scraper configs: %s", err.Error())
	}

	return ret
}

func (s *Manager) RefreshConfig() {
	*s.Paths = paths.NewPaths(s.Config.GetGeneratedPath(), s.Config.GetBlobsPath())
	config := s.Config
	if config.Validate() == nil {
		if err := fsutil.EnsureDir(s.Paths.Generated.Screenshots); err != nil {
			logger.Warnf("could not create directory for Screenshots: %v", err)
		}
		if err := fsutil.EnsureDir(s.Paths.Generated.Vtt); err != nil {
			logger.Warnf("could not create directory for VTT: %v", err)
		}
		if err := fsutil.EnsureDir(s.Paths.Generated.Markers); err != nil {
			logger.Warnf("could not create directory for Markers: %v", err)
		}
		if err := fsutil.EnsureDir(s.Paths.Generated.Transcodes); err != nil {
			logger.Warnf("could not create directory for Transcodes: %v", err)
		}
		if err := fsutil.EnsureDir(s.Paths.Generated.Downloads); err != nil {
			logger.Warnf("could not create directory for Downloads: %v", err)
		}
		if err := fsutil.EnsureDir(s.Paths.Generated.InteractiveHeatmap); err != nil {
			logger.Warnf("could not create directory for Interactive Heatmaps: %v", err)
		}
	}
}

// RefreshScraperCache refreshes the scraper cache. Call this when scraper
// configuration changes.
func (s *Manager) RefreshScraperCache() {
	s.ScraperCache = s.initScraperCache()
}

// RefreshStreamManager refreshes the stream manager. Call this when cache directory
// changes.
func (s *Manager) RefreshStreamManager() {
	// shutdown existing manager if needed
	if s.StreamManager != nil {
		s.StreamManager.Shutdown()
		s.StreamManager = nil
	}

	cacheDir := s.Config.GetCachePath()
	s.StreamManager = ffmpeg.NewStreamManager(cacheDir, s.FFMPEG, s.FFProbe, s.Config, s.ReadLockManager)
}

func setSetupDefaults(input *SetupInput) {
	if input.ConfigLocation == "" {
		input.ConfigLocation = filepath.Join(fsutil.GetHomeDirectory(), ".stash", "config.yml")
	}

	configDir := filepath.Dir(input.ConfigLocation)
	if input.GeneratedLocation == "" {
		input.GeneratedLocation = filepath.Join(configDir, "generated")
	}
	if input.CacheLocation == "" {
		input.CacheLocation = filepath.Join(configDir, "cache")
	}

	if input.DatabaseFile == "" {
		input.DatabaseFile = filepath.Join(configDir, "stash-go.sqlite")
	}

	if input.BlobsLocation == "" {
		input.BlobsLocation = filepath.Join(configDir, "blobs")
	}
}

func (s *Manager) Setup(ctx context.Context, input SetupInput) error {
	setSetupDefaults(&input)
	c := s.Config

	// create the config directory if it does not exist
	// don't do anything if config is already set in the environment
	if !config.FileEnvSet() {
		// #3304 - if config path is relative, it breaks the ffmpeg/ffprobe
		// paths since they must not be relative. The config file property is
		// resolved to an absolute path when stash is run normally, so convert
		// relative paths to absolute paths during setup.
		configFile, _ := filepath.Abs(input.ConfigLocation)

		configDir := filepath.Dir(configFile)

		if exists, _ := fsutil.DirExists(configDir); !exists {
			if err := os.MkdirAll(configDir, 0755); err != nil {
				return fmt.Errorf("error creating config directory: %v", err)
			}
		}

		if err := fsutil.Touch(configFile); err != nil {
			return fmt.Errorf("error creating config file: %v", err)
		}

		s.Config.SetConfigFile(configFile)
	}

	// create the generated directory if it does not exist
	if !c.HasOverride(config.Generated) {
		if exists, _ := fsutil.DirExists(input.GeneratedLocation); !exists {
			if err := os.MkdirAll(input.GeneratedLocation, 0755); err != nil {
				return fmt.Errorf("error creating generated directory: %v", err)
			}
		}

		s.Config.Set(config.Generated, input.GeneratedLocation)
	}

	// create the cache directory if it does not exist
	if !c.HasOverride(config.Cache) {
		if exists, _ := fsutil.DirExists(input.CacheLocation); !exists {
			if err := os.MkdirAll(input.CacheLocation, 0755); err != nil {
				return fmt.Errorf("error creating cache directory: %v", err)
			}
		}

		s.Config.Set(config.Cache, input.CacheLocation)
	}

	if input.StoreBlobsInDatabase {
		s.Config.Set(config.BlobsStorage, config.BlobStorageTypeDatabase)
	} else {
		if !c.HasOverride(config.BlobsPath) {
			if exists, _ := fsutil.DirExists(input.BlobsLocation); !exists {
				if err := os.MkdirAll(input.BlobsLocation, 0755); err != nil {
					return fmt.Errorf("error creating blobs directory: %v", err)
				}
			}

			s.Config.Set(config.BlobsPath, input.BlobsLocation)
		}

		s.Config.Set(config.BlobsStorage, config.BlobStorageTypeFilesystem)
	}

	// set the configuration
	if !c.HasOverride(config.Database) {
		s.Config.Set(config.Database, input.DatabaseFile)
	}

	s.Config.Set(config.Stash, input.Stashes)
	if err := s.Config.Write(); err != nil {
		return fmt.Errorf("error writing configuration file: %v", err)
	}

	// initialise the database
	if err := s.PostInit(ctx); err != nil {
		var migrationNeededErr *sqlite.MigrationNeededError
		if errors.As(err, &migrationNeededErr) {
			logger.Warn(err.Error())
		} else {
			return fmt.Errorf("error initializing the database: %v", err)
		}
	}

	s.Config.FinalizeSetup()

	if err := initFFMPEG(ctx); err != nil {
		return fmt.Errorf("error initializing FFMPEG subsystem: %v", err)
	}

	instance.Scanner = makeScanner(instance.Database, instance.PluginCache)

	return nil
}

func (s *Manager) validateFFMPEG() error {
	if s.FFMPEG == nil || s.FFProbe == "" {
		return errors.New("missing ffmpeg and/or ffprobe")
	}

	return nil
}

type MigrateInput struct {
	BackupPath string `json:"backupPath"`
}

func (s *Manager) Migrate(ctx context.Context, input MigrateInput) error {
	database := s.Database

	// always backup so that we can roll back to the previous version if
	// migration fails
	backupPath := input.BackupPath
	if backupPath == "" {
		backupPath = database.DatabaseBackupPath(s.Config.GetBackupDirectoryPath())
	} else {
		// check if backup path is a filename or path
		// filename goes into backup directory, path is kept as is
		filename := filepath.Base(backupPath)
		if backupPath == filename {
			backupPath = filepath.Join(s.Config.GetBackupDirectoryPathOrDefault(), filename)
		}
	}

	// perform database backup
	if err := database.Backup(backupPath); err != nil {
		return fmt.Errorf("error backing up database: %s", err)
	}

	if err := database.RunMigrations(); err != nil {
		errStr := fmt.Sprintf("error performing migration: %s", err)

		// roll back to the backed up version
		restoreErr := database.RestoreFromBackup(backupPath)
		if restoreErr != nil {
			errStr = fmt.Sprintf("ERROR: unable to restore database from backup after migration failure: %s\n%s", restoreErr.Error(), errStr)
		} else {
			errStr = "An error occurred migrating the database to the latest schema version. The backup database file was automatically renamed to restore the database.\n" + errStr
		}

		return errors.New(errStr)
	}

	// if no backup path was provided, then delete the created backup
	if input.BackupPath == "" {
		if err := os.Remove(backupPath); err != nil {
			logger.Warnf("error removing unwanted database backup (%s): %s", backupPath, err.Error())
		}
	}

	return nil
}

func (s *Manager) GetSystemStatus() *SystemStatus {
	database := s.Database
	status := SystemStatusEnumOk
	dbSchema := int(database.Version())
	dbPath := database.DatabasePath()
	appSchema := int(database.AppSchemaVersion())
	configFile := s.Config.GetConfigFile()

	if s.Config.IsNewSystem() {
		status = SystemStatusEnumSetup
	} else if dbSchema < appSchema {
		status = SystemStatusEnumNeedsMigration
	}

	return &SystemStatus{
		DatabaseSchema: &dbSchema,
		DatabasePath:   &dbPath,
		AppSchema:      appSchema,
		Status:         status,
		ConfigPath:     &configFile,
	}
}

// Shutdown gracefully stops the manager
func (s *Manager) Shutdown(code int) {
	// stop any profiling at exit
	pprof.StopCPUProfile()

	if s.StreamManager != nil {
		s.StreamManager.Shutdown()
		s.StreamManager = nil
	}

	// TODO: Each part of the manager needs to gracefully stop at some point
	// for now, we just close the database.
	err := s.Database.Close()
	if err != nil {
		logger.Errorf("Error closing database: %s", err)
		if code == 0 {
			os.Exit(1)
		}
	}

	os.Exit(code)
}
