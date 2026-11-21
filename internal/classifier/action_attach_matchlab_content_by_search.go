package classifier

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/classifier/classification"
	"github.com/bitmagnet-io/bitmagnet/internal/matchlab"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/tmdb"
)

// MatchLab's /match response, not the local parser, determines IMDb identity,
// movie/series type and any verified episode coordinates.
func matchlabContentType(r matchlab.Response) (model.ContentType, bool) {
	if r.IsSeries() {
		return model.ContentTypeTvShow, true
	}
	if _, ok := matchlab.IMDbID(r); ok {
		return model.ContentTypeMovie, true
	}
	return "", false
}

// Only expand a verifiable single-season interval. Cross-season SID endpoints
// lack the intermediate season boundaries; inventing them would be incorrect.
func applyMatchlabEpisodes(r matchlab.Response, cl *classification.Result) {
	cl.Episodes = nil // do not retain potentially contradictory local/hinted episodes
	if r.SIDStart == nil || r.SIDEnd == nil {
		return
	}
	start := strings.Split(*r.SIDStart, ":")
	end := strings.Split(*r.SIDEnd, ":")
	if start[0] != end[0] {
		return
	}
	if len(start) == 2 && len(end) == 2 && start[1] == end[1] {
		season, err := strconv.Atoi(start[1])
		if err == nil && season >= 0 && season <= 9999 {
			cl.Episodes = make(model.Episodes).AddSeason(season)
		}
		return
	}
	if len(start) != 3 || len(end) != 3 {
		return
	}
	s1, e1 := parseMatchlabInt(start[1]), parseMatchlabInt(start[2])
	s2, e2 := parseMatchlabInt(end[1]), parseMatchlabInt(end[2])
	if s1 < 0 || s1 > 9999 || s1 != s2 || e1 < 0 || e2 < e1 || e2 > 99999 || e2-e1 > 1000 {
		return
	}
	episodes := make(model.Episodes)
	for ep := e1; ep <= e2; ep++ {
		episodes.AddEpisode(s1, ep)
	}
	cl.Episodes = episodes
}

func parseMatchlabInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func overrideMatchlabParsed(ctx context.Context, api matchlab.Client, title string, cl *classification.Result) error {
	parsed, err := api.Parse(ctx, title)
	if err != nil {
		return err
	}
	if clean := strings.TrimSpace(parsed.Title); clean != "" {
		cl.BaseTitle = model.NewNullString(clean)
	}
	if parsed.Year != nil && *parsed.Year >= 1800 && *parsed.Year <= 2100 {
		cl.Date.Year = model.Year(*parsed.Year)
	}
	return nil
}

const attachMatchlabContentBySearchName = "attach_matchlab_content_by_search"

type attachMatchlabContentBySearchAction struct{}

func (attachMatchlabContentBySearchAction) name() string { return attachMatchlabContentBySearchName }

var attachMatchlabContentBySearchPayloadSpec = payloadLiteral[string]{
	literal:     attachMatchlabContentBySearchName,
	description: "Identify a release with MatchLab, then reuse metadata previously fetched from TMDB for its verified IMDb ID, or retrieve it from TMDB",
}

func (attachMatchlabContentBySearchAction) compileAction(ctx compilerContext) (action, error) {
	if _, err := attachMatchlabContentBySearchPayloadSpec.Unmarshal(ctx); err != nil {
		return action{}, ctx.error(err)
	}
	return action{run: func(ctx executionContext) (classification.Result, error) {
		cl := ctx.result
		if ctx.matchlabClient == nil {
			return cl, classification.ErrUnmatched
		}

		// Distinguish genuine no-match (no fallback) from transport failure
		// (retryable queue error). Never classify a server outage as unmatched.
		matched, found, err := ctx.matchlabClient.Match(ctx.Context, ctx.torrent.Name)
		if err != nil {
			return cl, fmt.Errorf("MatchLab identity lookup: %w", err)
		}
		if !found {
			return cl, classification.ErrUnmatched
		}

		contentType, typed := matchlabContentType(matched)
		if !typed {
			return cl, classification.ErrUnmatched
		}

		// MatchLab owns identity/episodes. Only request /parse if TMDB
		// cannot supply title metadata, avoiding an extra HTTP call per match.
		enrichFromParse := func() {
			parseErr := overrideMatchlabParsed(
				ctx.Context, ctx.matchlabClient, ctx.torrent.Name, &cl,
			)
			if parseErr != nil && ctx.logger != nil {
				ctx.logger.Warnw("MatchLab parse enrichment unavailable", "error", parseErr)
			}
		}
		cl.ContentType = model.NewNullContentType(contentType)
		if contentType == model.ContentTypeTvShow {
			applyMatchlabEpisodes(matched, &cl)
		} else {
			cl.Episodes = nil
		}

		imdb, hasIMDb := matchlab.IMDbID(matched)
		if !hasIMDb {
			// AniDB coordinates alone can identify the media type, but must
			// not be turned into an invented IMDb or TMDB ID.
			enrichFromParse()
			return cl, nil
		}
		// Metadata fetched from TMDB for this IMDb ID earlier is reused as
		// stored: no TMDB request for every torrent of an already known title.
		if local, found := reusableLocalTMDBContent(ctx, contentType, imdb); found {
			if local.Type != contentType {
				// A bare IMDb SID: the stored record clarifies a series.
				cl.ContentType = model.NewNullContentType(local.Type)
			}
			cl.AttachContent(&local)
			return cl, nil
		}

		content := model.Content{
			Type: contentType, Source: model.SourceImdb, ID: imdb,
			Title: ctx.torrent.Name,
		}
		metadataEnriched := false

		if ctx.tmdbEnabled && ctx.tmdbClient != nil {
			// Lookup by MatchLab's verified IMDb ID only. The same response
			// identifies the TMDB media type if MatchLab returned a bare SID.
			lookup, lookupErr := ctx.tmdbClient.FindByID(ctx.Context, tmdb.FindByIDRequest{
				ExternalSource: "imdb_id",
				ExternalID:     imdb,
			})
			if lookupErr != nil && !errors.Is(lookupErr, tmdb.ErrNotFound) {
				return cl, fmt.Errorf("TMDB external ID lookup for %s: %w", imdb, lookupErr)
			}

			var tmdbID int64
			switch contentType {
			case model.ContentTypeTvShow:
				if len(lookup.TvResults) > 0 {
					tmdbID = lookup.TvResults[0].ID
				}
			case model.ContentTypeMovie:
				if len(lookup.MovieResults) > 0 {
					tmdbID = lookup.MovieResults[0].ID
				} else if len(lookup.TvResults) > 0 {
					// A bare IMDb SID does not itself distinguish a film from
					// a series; TMDB may clarify the type, never the identity.
					contentType = model.ContentTypeTvShow
					content.Type = contentType
					cl.ContentType = model.NewNullContentType(contentType)
					tmdbID = lookup.TvResults[0].ID
				}
			}

			if tmdbID > 0 {
				var details model.Content
				var detailsErr error
				if contentType == model.ContentTypeTvShow {
					details, detailsErr = ctx.tmdbGetTVShowByTMDBID(tmdbID)
				} else {
					details, detailsErr = ctx.tmdbGetMovieByTMDBID(tmdbID)
				}
				if detailsErr == nil {
					// TMDB enriches metadata, not identity.
					details.Type = contentType
					details.Source = model.SourceImdb
					details.ID = imdb
					details.Attributes = append(details.Attributes, model.ContentAttribute{
						Source: model.SourceTmdb, Key: "id",
						Value: strconv.FormatInt(tmdbID, 10),
					})
					content = details
					metadataEnriched = true
				} else if !errors.Is(detailsErr, classification.ErrUnmatched) {
					return cl, fmt.Errorf("TMDB metadata lookup for %s: %w", imdb, detailsErr)
				}
			}
		}
		if !metadataEnriched {
			enrichFromParse()
			if cl.BaseTitle.Valid {
				content.Title = cl.BaseTitle.String
			}
		}
		cl.AttachContent(&content)
		return cl, nil
	}}, nil
}

// reusableLocalTMDBContent returns locally stored content for MatchLab's IMDb
// ID that was previously enriched from TMDB: canonical IMDb records carrying a
// TMDB ID attribute, and older TMDB records referencing the IMDb ID. Content
// stored without TMDB metadata is not reused, so TMDB can still enrich it.
// A bare IMDb SID (movie) may also be a stored series, as TMDB would clarify.
// Local search errors fall back to TMDB: the reuse is an optimisation only.
func reusableLocalTMDBContent(
	ctx executionContext, contentType model.ContentType, imdb string,
) (model.Content, bool) {
	if ctx.search == nil {
		return model.Content{}, false
	}
	types := []model.ContentType{contentType}
	if contentType == model.ContentTypeMovie {
		types = append(types, model.ContentTypeTvShow)
	}
	for _, candidateType := range types {
		content, err := ctx.search.ContentByID(ctx.Context, model.ContentRef{
			Type: candidateType, Source: model.SourceImdb, ID: imdb,
		})
		if errors.Is(err, classification.ErrUnmatched) {
			continue
		}
		if err != nil {
			if ctx.logger != nil {
				ctx.logger.Warnw("local content lookup unavailable, using TMDB",
					"imdb_id", imdb, "error", err)
			}
			return model.Content{}, false
		}
		if _, fromTMDB := content.Identifier(model.SourceTmdb); fromTMDB {
			return content, true
		}
	}
	return model.Content{}, false
}

func (attachMatchlabContentBySearchAction) JSONSchema() JSONSchema {
	return attachMatchlabContentBySearchPayloadSpec.JSONSchema()
}
