package scene

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/json"
	"github.com/stashapp/stash/pkg/models/jsonschema"
	"github.com/stashapp/stash/pkg/sliceutil/intslice"
	"github.com/stashapp/stash/pkg/utils"
)

type CoverGetter interface {
	GetCover(ctx context.Context, sceneID int) ([]byte, error)
}

type TagFinder interface {
	models.TagGetter
	FindBySceneID(ctx context.Context, sceneID int) ([]*models.Tag, error)
	FindBySceneMarkerID(ctx context.Context, sceneMarkerID int) ([]*models.Tag, error)
}

// ToBasicJSON converts a scene object into its JSON object equivalent. It
// does not convert the relationships to other objects, with the exception
// of cover image.
func ToBasicJSON(ctx context.Context, reader CoverGetter, scene *models.Scene) (*jsonschema.Scene, error) {
	newSceneJSON := jsonschema.Scene{
		Title:     scene.Title,
		Code:      scene.Code,
		URLs:      scene.URLs.List(),
		Details:   scene.Details,
		Director:  scene.Director,
		CreatedAt: json.JSONTime{Time: scene.CreatedAt},
		UpdatedAt: json.JSONTime{Time: scene.UpdatedAt},
	}

	if scene.Date != nil {
		newSceneJSON.Date = scene.Date.String()
	}

	if scene.Rating != nil {
		newSceneJSON.Rating = *scene.Rating
	}

	newSceneJSON.Organized = scene.Organized
	newSceneJSON.OCounter = scene.OCounter

	for _, f := range scene.Files.List() {
		newSceneJSON.Files = append(newSceneJSON.Files, f.Base().Path)
	}

	cover, err := reader.GetCover(ctx, scene.ID)
	if err != nil {
		logger.Errorf("Error getting scene cover: %v", err)
	}

	if len(cover) > 0 {
		newSceneJSON.Cover = utils.GetBase64StringFromData(cover)
	}

	var ret []models.StashID
	for _, stashID := range scene.StashIDs.List() {
		newJoin := models.StashID{
			StashID:  stashID.StashID,
			Endpoint: stashID.Endpoint,
		}
		ret = append(ret, newJoin)
	}

	newSceneJSON.StashIDs = ret

	return &newSceneJSON, nil
}

// GetStudioName returns the name of the provided scene's studio. It returns an
// empty string if there is no studio assigned to the scene.
func GetStudioName(ctx context.Context, reader models.StudioGetter, scene *models.Scene) (string, error) {
	if scene.StudioID != nil {
		studio, err := reader.Find(ctx, *scene.StudioID)
		if err != nil {
			return "", err
		}

		if studio != nil {
			return studio.Name, nil
		}
	}

	return "", nil
}

// GetTagNames returns a slice of tag names corresponding to the provided
// scene's tags.
func GetTagNames(ctx context.Context, reader TagFinder, scene *models.Scene) ([]string, error) {
	tags, err := reader.FindBySceneID(ctx, scene.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting scene tags: %v", err)
	}

	return getTagNames(tags), nil
}

func getTagNames(tags []*models.Tag) []string {
	var results []string
	for _, tag := range tags {
		if tag.Name != "" {
			results = append(results, tag.Name)
		}
	}

	return results
}

// GetDependentTagIDs returns a slice of unique tag IDs that this scene references.
func GetDependentTagIDs(ctx context.Context, tags TagFinder, markerReader models.SceneMarkerFinder, scene *models.Scene) ([]int, error) {
	var ret []int

	t, err := tags.FindBySceneID(ctx, scene.ID)
	if err != nil {
		return nil, err
	}

	for _, tt := range t {
		ret = intslice.IntAppendUnique(ret, tt.ID)
	}

	sm, err := markerReader.FindBySceneID(ctx, scene.ID)
	if err != nil {
		return nil, err
	}

	for _, smm := range sm {
		ret = intslice.IntAppendUnique(ret, smm.PrimaryTagID)
		smmt, err := tags.FindBySceneMarkerID(ctx, smm.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid tags for scene marker: %v", err)
		}

		for _, smmtt := range smmt {
			ret = intslice.IntAppendUnique(ret, smmtt.ID)
		}
	}

	return ret, nil
}

// GetSceneMoviesJSON returns a slice of SceneMovie JSON representation objects
// corresponding to the provided scene's scene movie relationships.
func GetSceneMoviesJSON(ctx context.Context, movieReader models.MovieGetter, scene *models.Scene) ([]jsonschema.SceneMovie, error) {
	sceneMovies := scene.Movies.List()

	var results []jsonschema.SceneMovie
	for _, sceneMovie := range sceneMovies {
		movie, err := movieReader.Find(ctx, sceneMovie.MovieID)
		if err != nil {
			return nil, fmt.Errorf("error getting movie: %v", err)
		}

		if movie != nil {
			sceneMovieJSON := jsonschema.SceneMovie{
				MovieName: movie.Name,
			}
			if sceneMovie.SceneIndex != nil {
				sceneMovieJSON.SceneIndex = *sceneMovie.SceneIndex
			}
			results = append(results, sceneMovieJSON)
		}
	}

	return results, nil
}

// GetDependentMovieIDs returns a slice of movie IDs that this scene references.
func GetDependentMovieIDs(ctx context.Context, scene *models.Scene) ([]int, error) {
	var ret []int

	m := scene.Movies.List()
	for _, mm := range m {
		ret = append(ret, mm.MovieID)
	}

	return ret, nil
}

// GetSceneMarkersJSON returns a slice of SceneMarker JSON representation
// objects corresponding to the provided scene's markers.
func GetSceneMarkersJSON(ctx context.Context, markerReader models.SceneMarkerFinder, tagReader TagFinder, scene *models.Scene) ([]jsonschema.SceneMarker, error) {
	sceneMarkers, err := markerReader.FindBySceneID(ctx, scene.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting scene markers: %v", err)
	}

	var results []jsonschema.SceneMarker

	for _, sceneMarker := range sceneMarkers {
		primaryTag, err := tagReader.Find(ctx, sceneMarker.PrimaryTagID)
		if err != nil {
			return nil, fmt.Errorf("invalid primary tag for scene marker: %v", err)
		}

		sceneMarkerTags, err := tagReader.FindBySceneMarkerID(ctx, sceneMarker.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid tags for scene marker: %v", err)
		}

		sceneMarkerJSON := jsonschema.SceneMarker{
			Title:      sceneMarker.Title,
			Seconds:    getDecimalString(sceneMarker.Seconds),
			PrimaryTag: primaryTag.Name,
			Tags:       getTagNames(sceneMarkerTags),
			CreatedAt:  json.JSONTime{Time: sceneMarker.CreatedAt},
			UpdatedAt:  json.JSONTime{Time: sceneMarker.UpdatedAt},
		}

		results = append(results, sceneMarkerJSON)
	}

	return results, nil
}

func getDecimalString(num float64) string {
	if num == 0 {
		return ""
	}

	precision := getPrecision(num)
	if precision == 0 {
		precision = 1
	}
	return fmt.Sprintf("%."+strconv.Itoa(precision)+"f", num)
}

func getPrecision(num float64) int {
	if num == 0 {
		return 0
	}

	e := 1.0
	p := 0
	for (math.Round(num*e) / e) != num {
		e *= 10
		p++
	}
	return p
}
