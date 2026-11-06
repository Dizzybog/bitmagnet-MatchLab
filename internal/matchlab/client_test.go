package matchlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testingClient(t *testing.T, url string) Client {
	t.Helper()
	c, err := NewClient(Config{URL: url, TimeoutSeconds: 2, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientMatchAndParse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("title") != "Some.Show.S01E02" {
			t.Errorf("original title was not forwarded: %q", r.URL.Query().Get("title"))
		}
		switch r.URL.Path {
		case "/match":
			_, _ = fmt.Fprint(w, `{"sid_start":"tt1234567:1:2","sid_end":"tt1234567:1:3",` +
				`"asid_start":null,"asid_end":null}`)
		case "/parse":
			_, _ = fmt.Fprint(w, `{"title":"Some Show","year":"2020"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := testingClient(t, server.URL)
	result, found, err := client.Match(context.Background(), "Some.Show.S01E02")
	if err != nil || !found {
		t.Fatalf("match: %+v %v %v", result, found, err)
	}
	if id, ok := IMDbID(result); !ok || id != "tt1234567" || !result.IsSeries() {
		t.Fatalf("invalid identity %+v", result)
	}
	parsed, err := client.Parse(context.Background(), "Some.Show.S01E02")
	if err != nil || parsed.Title != "Some Show" || parsed.Year == nil || *parsed.Year != 2020 {
		t.Fatalf("parse: %+v %v", parsed, err)
	}
}

func TestMatchNoFallbackOnEmptyAndInvalidIdentities(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body           string
		wantFound, wantError bool
	}{
		{"unmatched", `{"sid_start":null,"sid_end":null,` +
			`"asid_start":null,"asid_end":null}`, false, false},
		{"truncated no-match", `{"sid_start":null,"sid_end":null}`, false, true},
		{"movie", `{"sid_start":"tt1234567","sid_end":"tt1234567",` +
			`"asid_start":null,"asid_end":null}`, true, false},
		{"anidb only", `{"sid_start":null,"sid_end":null,` +
			`"asid_start":"14951:1","asid_end":"14951:1"}`, true, false},
		{"different IDs", `{"sid_start":"tt1234567","sid_end":"tt2222222",` +
			`"asid_start":null,"asid_end":null}`, false, true},
		{"malformed ID", `{"sid_start":"garbage","sid_end":"garbage",` +
			`"asid_start":null,"asid_end":null}`, false, true},
		{"inconsistent parts", `{"sid_start":"tt1234567:1:2","sid_end":"tt1234567",` +
			`"asid_start":null,"asid_end":null}`, false, true},
		{"missing end", `{"sid_start":"tt1234567"}`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			foundResponse, found, err := testingClient(t, srv.URL).Match(context.Background(), "Example")
			if found != tc.wantFound || (err != nil) != tc.wantError {
				t.Fatalf("response=%+v found=%v err=%v", foundResponse, found, err)
			}
			if tc.wantError && !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("want invalid response: %v", err)
			}
		})
	}
}

func TestHealthChecksReadinessNotLiveness(t *testing.T) {
	t.Parallel()
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		count++
		if count == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	c := testingClient(t, server.URL)
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("not-ready MatchLab must fail")
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConfigAndCanceledQueueRequest(t *testing.T) {
	t.Parallel()
	if c, err := NewClient(NewDefaultConfig()); err != nil || c != nil {
		t.Fatalf("disabled: %v %v", c, err)
	}
	for _, u := range []string{"ftp://example.org", "http://user:pw@example.org", "https://example.org?a=b"} {
		c, err := NewClient(Config{URL: u, TimeoutSeconds: 2, MaxConcurrent: 1})
		if err == nil || c != nil {
			t.Fatalf("invalid URL accepted %q", u)
		}
	}
	blocked := make(chan struct{})
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/match") {
			close(started)
			<-blocked
			_, _ = w.Write([]byte(`{"sid_start":null,"sid_end":null,"asid_start":null,"asid_end":null}`))
		}
	}))
	defer srv.Close()
	client := testingClient(t, srv.URL)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _, _ = client.Match(context.Background(), "busy")
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, _, err := client.Match(ctx, "second")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded request to cancel: %v", err)
	}
	close(blocked)
	<-finished
}
