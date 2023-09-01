package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	sqlite3mig "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
)

const (
	// Number of database connections to use
	// The same value is used for both the maximum and idle limit,
	// to prevent opening connections on the fly which has a notieable performance penalty.
	// Fewer connections use less memory, more connections increase performance,
	// but have diminishing returns.
	// 10 was found to be a good tradeoff.
	dbConns = 10
	// Idle connection timeout, in seconds
	// Closes a connection after a period of inactivity, which saves on memory and
	// causes the sqlite -wal and -shm files to be automatically deleted.
	dbConnTimeout = 30
)

var appSchemaVersion uint = 49

//go:embed migrations/*.sql
var migrationsBox embed.FS

var (
	// ErrDatabaseNotInitialized indicates that the database is not
	// initialized, usually due to an incomplete configuration.
	ErrDatabaseNotInitialized = errors.New("database not initialized")
)

// ErrMigrationNeeded indicates that a database migration is needed
// before the database can be initialized
type MigrationNeededError struct {
	CurrentSchemaVersion  uint
	RequiredSchemaVersion uint
}

func (e *MigrationNeededError) Error() string {
	return fmt.Sprintf("database schema version %d does not match required schema version %d", e.CurrentSchemaVersion, e.RequiredSchemaVersion)
}

type MismatchedSchemaVersionError struct {
	CurrentSchemaVersion  uint
	RequiredSchemaVersion uint
}

func (e *MismatchedSchemaVersionError) Error() string {
	return fmt.Sprintf("schema version %d is incompatible with required schema version %d", e.CurrentSchemaVersion, e.RequiredSchemaVersion)
}

type Database struct {
	Blobs          *BlobStore
	File           *FileStore
	Folder         *FolderStore
	Image          *ImageStore
	Gallery        *GalleryStore
	GalleryChapter *GalleryChapterStore
	Scene          *SceneStore
	SceneMarker    *SceneMarkerStore
	Performer      *PerformerStore
	SavedFilter    *SavedFilterStore
	Studio         *StudioStore
	Tag            *TagStore
	Movie          *MovieStore

	db     *sqlx.DB
	dbPath string

	schemaVersion uint

	lockChan chan struct{}
}

func NewDatabase() *Database {
	fileStore := NewFileStore()
	folderStore := NewFolderStore()
	blobStore := NewBlobStore(BlobStoreOptions{})

	ret := &Database{
		Blobs:          blobStore,
		File:           fileStore,
		Folder:         folderStore,
		Scene:          NewSceneStore(fileStore, blobStore),
		SceneMarker:    NewSceneMarkerStore(),
		Image:          NewImageStore(fileStore),
		Gallery:        NewGalleryStore(fileStore, folderStore),
		GalleryChapter: NewGalleryChapterStore(),
		Performer:      NewPerformerStore(blobStore),
		Studio:         NewStudioStore(blobStore),
		Tag:            NewTagStore(blobStore),
		Movie:          NewMovieStore(blobStore),
		SavedFilter:    NewSavedFilterStore(),
		lockChan:       make(chan struct{}, 1),
	}

	return ret
}

func (db *Database) SetBlobStoreOptions(options BlobStoreOptions) {
	*db.Blobs = *NewBlobStore(options)
}

// Ready returns an error if the database is not ready to begin transactions.
func (db *Database) Ready() error {
	if db.db == nil {
		return ErrDatabaseNotInitialized
	}

	return nil
}

// Open initializes the database. If the database is new, then it
// performs a full migration to the latest schema version. Otherwise, any
// necessary migrations must be run separately using RunMigrations.
// Returns true if the database is new.
func (db *Database) Open(dbPath string) error {
	db.lockNoCtx()
	defer db.unlock()

	db.dbPath = dbPath

	databaseSchemaVersion, err := db.getDatabaseSchemaVersion()
	if err != nil {
		return fmt.Errorf("getting database schema version: %w", err)
	}

	db.schemaVersion = databaseSchemaVersion

	if databaseSchemaVersion == 0 {
		// new database, just run the migrations
		if err := db.RunMigrations(); err != nil {
			return fmt.Errorf("error running initial schema migrations: %v", err)
		}
	} else {
		if databaseSchemaVersion > appSchemaVersion {
			return &MismatchedSchemaVersionError{
				CurrentSchemaVersion:  databaseSchemaVersion,
				RequiredSchemaVersion: appSchemaVersion,
			}
		}

		// if migration is needed, then don't open the connection
		if db.needsMigration() {
			return &MigrationNeededError{
				CurrentSchemaVersion:  databaseSchemaVersion,
				RequiredSchemaVersion: appSchemaVersion,
			}
		}
	}

	// RunMigrations may have opened a connection already
	if db.db == nil {
		const disableForeignKeys = false
		db.db, err = db.open(disableForeignKeys)
		if err != nil {
			return err
		}
	}

	return nil
}

// lock locks the database for writing.
// This method will block until the lock is acquired of the context is cancelled.
func (db *Database) lock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case db.lockChan <- struct{}{}:
		return nil
	}
}

// lock locks the database for writing. This method will block until the lock is acquired.
func (db *Database) lockNoCtx() {
	db.lockChan <- struct{}{}
}

// unlock unlocks the database
func (db *Database) unlock() {
	// will block the caller if the lock is not held, so check first
	select {
	case <-db.lockChan:
		return
	default:
		panic("database is not locked")
	}
}

func (db *Database) Close() error {
	db.lockNoCtx()
	defer db.unlock()

	if db.db != nil {
		if err := db.db.Close(); err != nil {
			return err
		}

		db.db = nil
	}

	return nil
}

func (db *Database) open(disableForeignKeys bool) (*sqlx.DB, error) {
	// https://github.com/mattn/go-sqlite3
	url := "file:" + db.dbPath + "?_journal=WAL&_sync=NORMAL&_busy_timeout=50"
	if !disableForeignKeys {
		url += "&_fk=true"
	}

	conn, err := sqlx.Open(sqlite3Driver, url)
	conn.SetMaxOpenConns(dbConns)
	conn.SetMaxIdleConns(dbConns)
	conn.SetConnMaxIdleTime(dbConnTimeout * time.Second)
	if err != nil {
		return nil, fmt.Errorf("db.Open(): %w", err)
	}

	return conn, nil
}

func (db *Database) Remove() error {
	databasePath := db.dbPath
	err := db.Close()

	if err != nil {
		return errors.New("Error closing database: " + err.Error())
	}

	err = os.Remove(databasePath)
	if err != nil {
		return errors.New("Error removing database: " + err.Error())
	}

	// remove the -shm, -wal files ( if they exist )
	walFiles := []string{databasePath + "-shm", databasePath + "-wal"}
	for _, wf := range walFiles {
		if exists, _ := fsutil.FileExists(wf); exists {
			err = os.Remove(wf)
			if err != nil {
				return errors.New("Error removing database: " + err.Error())
			}
		}
	}

	return nil
}

func (db *Database) Reset() error {
	databasePath := db.dbPath
	if err := db.Remove(); err != nil {
		return err
	}

	if err := db.Open(databasePath); err != nil {
		return fmt.Errorf("[reset DB] unable to initialize: %w", err)
	}

	return nil
}

// Backup the database. If db is nil, then uses the existing database
// connection.
func (db *Database) Backup(backupPath string) error {
	thisDB := db.db
	if thisDB == nil {
		var err error
		thisDB, err = sqlx.Connect(sqlite3Driver, "file:"+db.dbPath+"?_fk=true")
		if err != nil {
			return fmt.Errorf("open database %s failed: %v", db.dbPath, err)
		}
		defer thisDB.Close()
	}

	logger.Infof("Backing up database into: %s", backupPath)
	_, err := thisDB.Exec(`VACUUM INTO "` + backupPath + `"`)
	if err != nil {
		return fmt.Errorf("vacuum failed: %v", err)
	}

	return nil
}

func (db *Database) Anonymise(outPath string) error {
	anon, err := NewAnonymiser(db, outPath)

	if err != nil {
		return err
	}

	return anon.Anonymise(context.Background())
}

func (db *Database) RestoreFromBackup(backupPath string) error {
	logger.Infof("Restoring backup database %s into %s", backupPath, db.dbPath)
	return os.Rename(backupPath, db.dbPath)
}

// Migrate the database
func (db *Database) needsMigration() bool {
	return db.schemaVersion != appSchemaVersion
}

func (db *Database) AppSchemaVersion() uint {
	return appSchemaVersion
}

func (db *Database) DatabasePath() string {
	return db.dbPath
}

func (db *Database) DatabaseBackupPath(backupDirectoryPath string) string {
	fn := fmt.Sprintf("%s.%d.%s", filepath.Base(db.dbPath), db.schemaVersion, time.Now().Format("20060102_150405"))

	if backupDirectoryPath != "" {
		return filepath.Join(backupDirectoryPath, fn)
	}

	return fn
}

func (db *Database) AnonymousDatabasePath(backupDirectoryPath string) string {
	fn := fmt.Sprintf("%s.anonymous.%d.%s", filepath.Base(db.dbPath), db.schemaVersion, time.Now().Format("20060102_150405"))

	if backupDirectoryPath != "" {
		return filepath.Join(backupDirectoryPath, fn)
	}

	return fn
}

func (db *Database) Version() uint {
	return db.schemaVersion
}

func (db *Database) getMigrate() (*migrate.Migrate, error) {
	migrations, err := iofs.New(migrationsBox, "migrations")
	if err != nil {
		return nil, err
	}

	const disableForeignKeys = true
	conn, err := db.open(disableForeignKeys)
	if err != nil {
		return nil, err
	}

	driver, err := sqlite3mig.WithInstance(conn.DB, &sqlite3mig.Config{})
	if err != nil {
		return nil, err
	}

	// use sqlite3Driver so that migration has access to durationToTinyInt
	return migrate.NewWithInstance(
		"iofs",
		migrations,
		db.dbPath,
		driver,
	)
}

func (db *Database) getDatabaseSchemaVersion() (uint, error) {
	m, err := db.getMigrate()
	if err != nil {
		return 0, err
	}
	defer m.Close()

	ret, _, _ := m.Version()
	return ret, nil
}

// Migrate the database
func (db *Database) RunMigrations() error {
	ctx := context.Background()

	m, err := db.getMigrate()
	if err != nil {
		return err
	}
	defer m.Close()

	databaseSchemaVersion, _, _ := m.Version()
	stepNumber := appSchemaVersion - databaseSchemaVersion
	if stepNumber != 0 {
		logger.Infof("Migrating database from version %d to %d", databaseSchemaVersion, appSchemaVersion)

		// run each migration individually, and run custom migrations as needed
		var i uint = 1
		for ; i <= stepNumber; i++ {
			newVersion := databaseSchemaVersion + i

			// run pre migrations as needed
			if err := db.runCustomMigrations(ctx, preMigrations[newVersion]); err != nil {
				return fmt.Errorf("running pre migrations for schema version %d: %w", newVersion, err)
			}

			err = m.Steps(1)
			if err != nil {
				// migration failed
				return err
			}

			// run post migrations as needed
			if err := db.runCustomMigrations(ctx, postMigrations[newVersion]); err != nil {
				return fmt.Errorf("running post migrations for schema version %d: %w", newVersion, err)
			}
		}
	}

	// update the schema version
	db.schemaVersion, _, _ = m.Version()

	// re-initialise the database
	const disableForeignKeys = false
	db.db, err = db.open(disableForeignKeys)
	if err != nil {
		return fmt.Errorf("re-initializing the database: %w", err)
	}

	// optimize database after migration
	err = db.Optimise(ctx)
	if err != nil {
		logger.Warnf("error while performing post-migration optimisation: %v", err)
	}

	return nil
}

func (db *Database) Optimise(ctx context.Context) error {
	logger.Info("Optimising database")

	err := db.Analyze(ctx)
	if err != nil {
		return fmt.Errorf("performing optimization: %w", err)
	}

	err = db.Vacuum(ctx)
	if err != nil {
		return fmt.Errorf("performing vacuum: %w", err)
	}

	return nil
}

// Vacuum runs a VACUUM on the database, rebuilding the database file into a minimal amount of disk space.
func (db *Database) Vacuum(ctx context.Context) error {
	_, err := db.db.ExecContext(ctx, "VACUUM")
	return err
}

// Analyze runs an ANALYZE on the database to improve query performance.
func (db *Database) Analyze(ctx context.Context) error {
	_, err := db.db.ExecContext(ctx, "ANALYZE")
	return err
}

func (db *Database) ExecSQL(ctx context.Context, query string, args []interface{}) (*int64, *int64, error) {
	wrapper := dbWrapper{}

	result, err := wrapper.Exec(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}

	var rowsAffected *int64
	ra, err := result.RowsAffected()
	if err == nil {
		rowsAffected = &ra
	}

	var lastInsertId *int64
	li, err := result.LastInsertId()
	if err == nil {
		lastInsertId = &li
	}

	return rowsAffected, lastInsertId, nil
}

func (db *Database) QuerySQL(ctx context.Context, query string, args []interface{}) ([]string, [][]interface{}, error) {
	wrapper := dbWrapper{}

	rows, err := wrapper.QueryxContext(ctx, query, args...)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var ret [][]interface{}

	for rows.Next() {
		row, err := rows.SliceScan()
		if err != nil {
			return nil, nil, err
		}
		ret = append(ret, row)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return cols, ret, nil
}

func (db *Database) runCustomMigrations(ctx context.Context, fns []customMigrationFunc) error {
	for _, fn := range fns {
		if err := db.runCustomMigration(ctx, fn); err != nil {
			return err
		}
	}

	return nil
}

func (db *Database) runCustomMigration(ctx context.Context, fn customMigrationFunc) error {
	const disableForeignKeys = false
	d, err := db.open(disableForeignKeys)
	if err != nil {
		return err
	}

	defer d.Close()
	if err := fn(ctx, d); err != nil {
		return err
	}

	return nil
}
