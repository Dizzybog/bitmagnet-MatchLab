package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/classifier/classification"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

// matchlabClient calls MatchLab's existing four-field /match API.
// An empty MATCHLAB_URL disables this optional integration.
type matchlabClient struct {
	endpoint      string
	parseEndpoint string
	httpClient    *http.Client
}

type matchlabResponse struct {
	SIDStart  *string `json:"sid_start"`
	SIDEnd    *string `json:"sid_end"`
	ASIDStart *string `json:"asid_start"`
	ASIDEnd   *string `json:"asid_end"`
}

func newMatchlabClient(raw string) (*matchlabClient, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("MATCHLAB_URL must be an HTTP(S) base URL without credentials, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/match"
	matchURL := u.String()
	u.Path = strings.TrimSuffix(u.Path, "/match") + "/parse"
	return &matchlabClient{endpoint: matchURL, parseEndpoint: u.String(), httpClient: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (m *matchlabClient) match(ctx context.Context, title string) (matchlabResponse, bool, error) {
	if m == nil {
		return matchlabResponse{}, false, nil
	}
	u, err := url.Parse(m.endpoint)
	if err != nil {
		return matchlabResponse{}, false, err
	}
	q := u.Query()
	q.Set("title", title)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return matchlabResponse{}, false, err
	}
	res, err := m.httpClient.Do(req)
	if err != nil {
		return matchlabResponse{}, false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return matchlabResponse{}, false, fmt.Errorf("MatchLab HTTP %d", res.StatusCode)
	}
	var payload matchlabResponse
	if err = json.NewDecoder(io.LimitReader(res.Body, 16*1024)).Decode(&payload); err != nil {
		return matchlabResponse{}, false, fmt.Errorf("invalid MatchLab response: %w", err)
	}
	if hasMatchlabASID(payload) {
		// AniDB coordinates identify a series even if IMDb coordinates are absent.
		return payload, true, nil
	}
	if payload.SIDStart == nil || payload.SIDEnd == nil {
		return matchlabResponse{}, false, nil
	}
	start, end := strings.TrimSpace(*payload.SIDStart), strings.TrimSpace(*payload.SIDEnd)
	if start == "" || end == "" {
		return matchlabResponse{}, false, nil
	}
	endID, _, _ := strings.Cut(end, ":")
	imdb, _, _ := strings.Cut(start, ":")
	if endID != imdb {
		return matchlabResponse{}, false, nil
	}
	if !validMatchlabIMDb(imdb) {
		return matchlabResponse{}, false, errors.New("invalid IMDb identifier from MatchLab")
	}
	return payload, true, nil
}

func validMatchlabIMDb(imdb string) bool {
	if len(imdb) < 9 || len(imdb) > 12 || !strings.HasPrefix(imdb, "tt") {
		return false
	}
	for _, digit := range imdb[2:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func hasMatchlabASID(p matchlabResponse) bool {
	return (p.ASIDStart != nil && strings.TrimSpace(*p.ASIDStart) != "") ||
		(p.ASIDEnd != nil && strings.TrimSpace(*p.ASIDEnd) != "")
}

// Classification precedence: IMDb episode coordinates, then AniDB,
// then a bare IMDb title. The existing Bitmagnet parser is not authoritative.
func matchlabContentType(p matchlabResponse) (model.ContentType, bool) {
	if p.SIDStart != nil && strings.TrimSpace(*p.SIDStart) != "" {
		sid := strings.Split(strings.TrimSpace(*p.SIDStart), ":")
		if len(sid) > 1 {
			return model.ContentTypeTvShow, true
		}
	}
	if p.SIDEnd != nil && strings.TrimSpace(*p.SIDEnd) != "" {
		sid := strings.Split(strings.TrimSpace(*p.SIDEnd), ":")
		if len(sid) > 1 {
			return model.ContentTypeTvShow, true
		}
	}
	if hasMatchlabASID(p) {
		return model.ContentTypeTvShow, true
	}
	if p.SIDStart != nil && validMatchlabIMDb(strings.TrimSpace(*p.SIDStart)) {
		return model.ContentTypeMovie, true
	}
	return "", false
}

type matchlabParsed struct {
	Title string `json:"title"`
	Year  *int   `json:"year"`
}

// Parsing overrides only existing Bitmagnet fields, never adds a new schema field.
func (m *matchlabClient) parse(ctx context.Context, title string) (matchlabParsed, error) {
	u, err := url.Parse(m.parseEndpoint)
	if err != nil {
		return matchlabParsed{}, err
	}
	q := u.Query()
	q.Set("title", title)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return matchlabParsed{}, err
	}
	res, err := m.httpClient.Do(req)
	if err != nil {
		return matchlabParsed{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return matchlabParsed{}, fmt.Errorf("MatchLab parse HTTP %d", res.StatusCode)
	}
	var parsed matchlabParsed
	err = json.NewDecoder(io.LimitReader(res.Body, 16*1024)).Decode(&parsed)
	return parsed, err
}

func (m *matchlabClient) overrideParsed(ctx context.Context, title string, cl *classification.Result) {
	parsed, err := m.parse(ctx, title)
	if err != nil {
		return
	}
	if strings.TrimSpace(parsed.Title) != "" {
		cl.BaseTitle = model.NewNullString(parsed.Title)
	}
	if parsed.Year != nil && *parsed.Year >= 1800 && *parsed.Year <= 2100 {
		cl.Date.Year = model.Year(*parsed.Year)
	}

}

// MatchLab /match is authoritative for season/episode coordinates.
// Do not synthesize cross-season ranges or change values on malformed SIDs.
func applyMatchlabEpisodes(payload matchlabResponse, cl *classification.Result) {
	if payload.SIDStart == nil || payload.SIDEnd == nil {
		return
	}
	start := strings.Split(*payload.SIDStart, ":")
	end := strings.Split(*payload.SIDEnd, ":")
	if len(start) != 3 || len(end) != 3 || start[0] != end[0] {
		return
	}
	s1, err1 := strconv.Atoi(start[1])
	e1, err2 := strconv.Atoi(start[2])
	s2, err3 := strconv.Atoi(end[1])
	e2, err4 := strconv.Atoi(end[2])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil ||
		s1 < 0 || s1 > 9999 || s2 != s1 || e1 < 0 || e2 < e1 || e2 > 99999 || e2-e1 > 1000 {
		return
	}
	episodes := make(model.Episodes)
	for episode := e1; episode <= e2; episode++ {
		episodes.AddEpisode(s1, episode)
	}
	cl.Episodes = episodes
}

const attachMatchlabContentBySearchName = "attach_matchlab_content_by_search"

type attachMatchlabContentBySearchAction struct{}

func (attachMatchlabContentBySearchAction) name() string { return attachMatchlabContentBySearchName }

var attachMatchlabContentBySearchPayloadSpec = payloadLiteral[string]{
	literal:     attachMatchlabContentBySearchName,
	description: "Identify a torrent using the optional MatchLab API and attach IMDb content",
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
		matched, found, err := ctx.matchlabClient.match(ctx.Context, ctx.torrent.Name)
		if err != nil {
			return cl, classification.ErrUnmatched
		}
		if !found {
			return cl, classification.ErrUnmatched
		}
		ctx.matchlabClient.overrideParsed(ctx.Context, ctx.torrent.Name, &cl)
		contentType, typed := matchlabContentType(matched)
		if !typed {
			return cl, classification.ErrUnmatched
		}
		cl.ContentType = model.NewNullContentType(contentType)
		if contentType == model.ContentTypeTvShow {
			applyMatchlabEpisodes(matched, &cl)
		} else {
			cl.Episodes = nil
		}
		// An AniDB-only match can classify the torrent as a series, but it
		// cannot be enriched by IMDb ID until MatchLab supplies an SID.
		if matched.SIDStart == nil || !validMatchlabIMDb(strings.Split(*matched.SIDStart, ":")[0]) {
			return cl, nil
		}
		imdb, _, _ := strings.Cut(*matched.SIDStart, ":")
		// MatchLab alone determines identity. TMDB is queried strictly by IMDb ID
		// for richer metadata; no title-based TMDB search is performed.
		content := model.Content{Type: contentType, Source: model.SourceImdb, ID: imdb, Title: ctx.torrent.Name}
		if cl.BaseTitle.Valid {
			content.Title = cl.BaseTitle.String
		}
		ref := model.ContentRef{Type: contentType, Source: model.SourceImdb, ID: imdb}
		if ctx.tmdbClient != nil {
			if tmdbID, lookupErr := ctx.tmdbGetTMDBIDByExternalID(ref); lookupErr == nil {
				var details model.Content
				var detailsErr error
				if contentType == model.ContentTypeTvShow {
					details, detailsErr = ctx.tmdbGetTVShowByTMDBID(tmdbID)
				} else {
					details, detailsErr = ctx.tmdbGetMovieByTMDBID(tmdbID)
				}
				if detailsErr == nil {
					content = details
				}
			}
		}
		cl.AttachContent(&content)
		return cl, nil
	}}, nil
}
func (attachMatchlabContentBySearchAction) JSONSchema() JSONSchema {
	return attachMatchlabContentBySearchPayloadSpec.JSONSchema()
}

func matchlabFromEnvironment() (*matchlabClient, error) {
	return newMatchlabClient(os.Getenv("MATCHLAB_URL"))
}
