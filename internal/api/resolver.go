package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/stashapp/stash/internal/build"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/scraper"
	"github.com/stashapp/stash/pkg/txn"
)

var (
	// ErrNotImplemented is an error which means the given functionality isn't implemented by the API.
	ErrNotImplemented = errors.New("not implemented")

	// ErrNotSupported is returned whenever there's a test, which can be used to guard against the error,
	// but the given parameters aren't supported by the system.
	ErrNotSupported = errors.New("not supported")

	// ErrInput signifies errors where the input isn't valid for some reason. And no more specific error exists.
	ErrInput = errors.New("input error")
)

type hookExecutor interface {
	ExecutePostHooks(ctx context.Context, id int, hookType plugin.HookTriggerEnum, input interface{}, inputFields []string)
}

type Resolver struct {
	txnManager     txn.Manager
	repository     manager.Repository
	sceneService   manager.SceneService
	imageService   manager.ImageService
	galleryService manager.GalleryService

	hookExecutor hookExecutor
}

func (r *Resolver) scraperCache() *scraper.Cache {
	return manager.GetInstance().ScraperCache
}

func (r *Resolver) Gallery() GalleryResolver {
	return &galleryResolver{r}
}
func (r *Resolver) GalleryChapter() GalleryChapterResolver {
	return &galleryChapterResolver{r}
}
func (r *Resolver) Mutation() MutationResolver {
	return &mutationResolver{r}
}
func (r *Resolver) Performer() PerformerResolver {
	return &performerResolver{r}
}
func (r *Resolver) Query() QueryResolver {
	return &queryResolver{r}
}
func (r *Resolver) Scene() SceneResolver {
	return &sceneResolver{r}
}
func (r *Resolver) Image() ImageResolver {
	return &imageResolver{r}
}
func (r *Resolver) SceneMarker() SceneMarkerResolver {
	return &sceneMarkerResolver{r}
}
func (r *Resolver) Studio() StudioResolver {
	return &studioResolver{r}
}
func (r *Resolver) Movie() MovieResolver {
	return &movieResolver{r}
}
func (r *Resolver) Subscription() SubscriptionResolver {
	return &subscriptionResolver{r}
}
func (r *Resolver) Tag() TagResolver {
	return &tagResolver{r}
}
func (r *Resolver) SavedFilter() SavedFilterResolver {
	return &savedFilterResolver{r}
}

type mutationResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }

type galleryResolver struct{ *Resolver }
type galleryChapterResolver struct{ *Resolver }
type performerResolver struct{ *Resolver }
type sceneResolver struct{ *Resolver }
type sceneMarkerResolver struct{ *Resolver }
type imageResolver struct{ *Resolver }
type studioResolver struct{ *Resolver }
type movieResolver struct{ *Resolver }
type tagResolver struct{ *Resolver }
type savedFilterResolver struct{ *Resolver }

func (r *Resolver) withTxn(ctx context.Context, fn func(ctx context.Context) error) error {
	return txn.WithTxn(ctx, r.txnManager, fn)
}

func (r *Resolver) withReadTxn(ctx context.Context, fn func(ctx context.Context) error) error {
	return txn.WithReadTxn(ctx, r.txnManager, fn)
}

func (r *queryResolver) MarkerWall(ctx context.Context, q *string) (ret []*models.SceneMarker, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.SceneMarker.Wall(ctx, q)
		return err
	}); err != nil {
		return nil, err
	}
	return ret, nil
}

func (r *queryResolver) SceneWall(ctx context.Context, q *string) (ret []*models.Scene, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Scene.Wall(ctx, q)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) MarkerStrings(ctx context.Context, q *string, sort *string) (ret []*models.MarkerStringsResultType, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.SceneMarker.GetMarkerStrings(ctx, q, sort)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) Stats(ctx context.Context) (*StatsResultType, error) {
	var ret StatsResultType
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		repo := r.repository
		scenesQB := repo.Scene
		imageQB := repo.Image
		galleryQB := repo.Gallery
		studiosQB := repo.Studio
		performersQB := repo.Performer
		moviesQB := repo.Movie
		tagsQB := repo.Tag
		scenesCount, _ := scenesQB.Count(ctx)
		scenesSize, _ := scenesQB.Size(ctx)
		scenesDuration, _ := scenesQB.Duration(ctx)
		imageCount, _ := imageQB.Count(ctx)
		imageSize, _ := imageQB.Size(ctx)
		galleryCount, _ := galleryQB.Count(ctx)
		performersCount, _ := performersQB.Count(ctx)
		studiosCount, _ := studiosQB.Count(ctx)
		moviesCount, _ := moviesQB.Count(ctx)
		tagsCount, _ := tagsQB.Count(ctx)
		totalOCount, _ := scenesQB.OCount(ctx)
		totalPlayDuration, _ := scenesQB.PlayDuration(ctx)
		totalPlayCount, _ := scenesQB.PlayCount(ctx)
		uniqueScenePlayCount, _ := scenesQB.UniqueScenePlayCount(ctx)

		ret = StatsResultType{
			SceneCount:        scenesCount,
			ScenesSize:        scenesSize,
			ScenesDuration:    scenesDuration,
			ImageCount:        imageCount,
			ImagesSize:        imageSize,
			GalleryCount:      galleryCount,
			PerformerCount:    performersCount,
			StudioCount:       studiosCount,
			MovieCount:        moviesCount,
			TagCount:          tagsCount,
			TotalOCount:       totalOCount,
			TotalPlayDuration: totalPlayDuration,
			TotalPlayCount:    totalPlayCount,
			ScenesPlayed:      uniqueScenePlayCount,
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &ret, nil
}

func (r *queryResolver) Version(ctx context.Context) (*Version, error) {
	version, hash, buildtime := build.Version()

	return &Version{
		Version:   &version,
		Hash:      hash,
		BuildTime: buildtime,
	}, nil
}

func (r *queryResolver) Latestversion(ctx context.Context) (*LatestVersion, error) {
	latestRelease, err := GetLatestRelease(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Errorf("Error while retrieving latest version: %v", err)
		}
		return nil, err
	}
	logger.Infof("Retrieved latest version: %s (%s)", latestRelease.Version, latestRelease.ShortHash)

	return &LatestVersion{
		Version:     latestRelease.Version,
		Shorthash:   latestRelease.ShortHash,
		ReleaseDate: latestRelease.Date,
		URL:         latestRelease.Url,
	}, nil
}

func (r *mutationResolver) ExecSQL(ctx context.Context, sql string, args []interface{}) (*SQLExecResult, error) {
	var rowsAffected *int64
	var lastInsertID *int64

	db := manager.GetInstance().Database
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		var err error
		rowsAffected, lastInsertID, err = db.ExecSQL(ctx, sql, args)
		return err
	}); err != nil {
		return nil, err
	}

	return &SQLExecResult{
		RowsAffected: rowsAffected,
		LastInsertID: lastInsertID,
	}, nil
}

func (r *mutationResolver) QuerySQL(ctx context.Context, sql string, args []interface{}) (*SQLQueryResult, error) {
	var cols []string
	var rows [][]interface{}

	db := manager.GetInstance().Database
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		var err error
		cols, rows, err = db.QuerySQL(ctx, sql, args)
		return err
	}); err != nil {
		return nil, err
	}

	return &SQLQueryResult{
		Columns: cols,
		Rows:    rows,
	}, nil
}

// Get scene marker tags which show up under the video.
func (r *queryResolver) SceneMarkerTags(ctx context.Context, scene_id string) ([]*SceneMarkerTag, error) {
	sceneID, err := strconv.Atoi(scene_id)
	if err != nil {
		return nil, err
	}

	var keys []int
	tags := make(map[int]*SceneMarkerTag)

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		sceneMarkers, err := r.repository.SceneMarker.FindBySceneID(ctx, sceneID)
		if err != nil {
			return err
		}

		tqb := r.repository.Tag
		for _, sceneMarker := range sceneMarkers {
			markerPrimaryTag, err := tqb.Find(ctx, sceneMarker.PrimaryTagID)
			if err != nil {
				return err
			}

			if markerPrimaryTag == nil {
				return fmt.Errorf("tag with id %d not found", sceneMarker.PrimaryTagID)
			}

			_, hasKey := tags[markerPrimaryTag.ID]
			if !hasKey {
				sceneMarkerTag := &SceneMarkerTag{Tag: markerPrimaryTag}
				tags[markerPrimaryTag.ID] = sceneMarkerTag
				keys = append(keys, markerPrimaryTag.ID)
			}
			tags[markerPrimaryTag.ID].SceneMarkers = append(tags[markerPrimaryTag.ID].SceneMarkers, sceneMarker)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// Sort so that primary tags that show up earlier in the video are first.
	sort.Slice(keys, func(i, j int) bool {
		a := tags[keys[i]]
		b := tags[keys[j]]
		return a.SceneMarkers[0].Seconds < b.SceneMarkers[0].Seconds
	})

	var result []*SceneMarkerTag
	for _, key := range keys {
		result = append(result, tags[key])
	}

	return result, nil
}

func firstError(errs []error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	return nil
}
