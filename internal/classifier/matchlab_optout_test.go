package classifier

import (
	"context"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNativeSearchAfterMatchLabDisconnectWithStaleQueuedFlag(t *testing.T) {
	t.Parallel()

	mocks := newTestClassifierMocks(t)
	source, err := (yamlSourceProvider{rawSourceProvider: coreSourceProvider{}}).source()
	require.NoError(t, err)
	workflow, err := mocks.compiler.Compile(source)
	require.NoError(t, err)

	torrent := model.Torrent{
		Name:        "The Regular Local Movie (2000).mkv",
		FilesStatus: model.FilesStatusSingle,
		Extension:   model.NewNullString("mkv"),
		Size:        1000000000,
	}
	content := model.Content{
		Type: model.ContentTypeMovie, Source: "local", ID: "42",
		Title: "The Regular Local Movie", ReleaseYear: 2000,
	}
	mocks.search.On("ContentBySearch", mock.Anything, model.ContentTypeMovie,
		"The Regular Local Movie", model.Year(2000)).Return(content, nil).Once()

	// A process-torrent job persisted while MatchLab was configured may
	// contain matchlab_enabled=true even after its URL has been removed.
	// Native search must work without MatchLab or TMDB network access.
	result, err := workflow.Run(context.Background(), "default", Flags{
		"matchlab_enabled": true,
		"local_search_enabled": true,
		"apis_enabled": false,
	}, torrent)
	require.NoError(t, err)
	require.NotNil(t, result.Content)
	require.Equal(t, "local", result.Content.Source)
	require.Equal(t, "42", result.Content.ID)
}

func TestNativeModePreservesPreviouslyStoredMatchLabIMDbContent(t *testing.T) {
	t.Parallel()

	mocks := newTestClassifierMocks(t)
	source, err := (yamlSourceProvider{rawSourceProvider: coreSourceProvider{}}).source()
	require.NoError(t, err)
	workflow, err := mocks.compiler.Compile(source)
	require.NoError(t, err)

	stored := model.Content{Type: model.ContentTypeMovie, Source: model.SourceImdb,
		ID: "tt1234567", Title: "Verified Movie"}
	torrent := model.Torrent{
		Name: "Verified.Movie.2024.mkv",
		Hint: model.TorrentHint{
			ContentType: model.ContentTypeMovie,
			ContentSource: model.NewNullString(model.SourceImdb),
			ContentID: model.NewNullString("tt1234567"),
		},
		Contents: []model.TorrentContent{{
			ContentType: model.NewNullContentType(model.ContentTypeMovie),
			ContentSource: model.NewNullString(model.SourceImdb),
			ContentID: model.NewNullString("tt1234567"),
			Content: stored,
		}},
	}
	// The normal Bitmagnet classification path reuses an existing verified
	// hint. Disconnecting MatchLab must not erase usable stored metadata.
	result, err := workflow.Run(context.Background(), "default", Flags{
		"matchlab_enabled": true,
		"local_search_enabled": true,
		"apis_enabled": false,
	}, torrent)
	require.NoError(t, err)
	require.NotNil(t, result.Content)
	require.Equal(t, stored.Source, result.Content.Source)
	require.Equal(t, stored.ID, result.Content.ID)
}

func TestDisconnectedMatchLabDoesNotForceNativeSearchAgainstUserChoice(t *testing.T) {
	t.Parallel()

	mocks := newTestClassifierMocks(t)
	source, err := (yamlSourceProvider{rawSourceProvider: coreSourceProvider{}}).source()
	require.NoError(t, err)
	workflow, err := mocks.compiler.Compile(source)
	require.NoError(t, err)

	torrent := model.Torrent{
		Name: "The Regular Movie (2000).mkv",
		FilesStatus: model.FilesStatusSingle,
		Extension: model.NewNullString("mkv"),
		Size: 1000000000,
	}
	result, err := workflow.Run(context.Background(), "default", Flags{
		"matchlab_enabled": true,
		"local_search_enabled": false,
		"apis_enabled": false,
	}, torrent)
	require.NoError(t, err)
	require.Nil(t, result.Content)
}
