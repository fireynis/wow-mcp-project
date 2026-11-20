package forevercompanion

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "forevercompanion", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGetDropsMetaKeepsAttribution(t *testing.T) {
	var gotPath, gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotUA = r.URL.Path, r.URL.RawQuery, r.Header.Get("User-Agent")
		w.Write(fixture(t, "dungeons.json"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/api/v1"}
	a, err := c.Get(context.Background(), "/dungeons", url.Values{"faction": {"alliance"}, "minLevel": {"15"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/dungeons" || gotQuery != "faction=alliance&minLevel=15" {
		t.Errorf("request = %s?%s", gotPath, gotQuery)
	}
	if !strings.HasPrefix(gotUA, "wow-mcp/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if !strings.HasPrefix(string(a.Data), `[{"id":"hall-of-thanes"`) {
		t.Errorf("data = %.80s", a.Data)
	}
	if strings.Contains(string(a.Data), "attribution") {
		t.Error("data still carries the meta block")
	}
	if !strings.Contains(a.Attribution, "https://forevercompanion.io") || !strings.Contains(a.Attribution, "link back") {
		t.Errorf("attribution = %q", a.Attribution)
	}
}

func TestGetKeepsPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture(t, "quests.json"))
	}))
	defer srv.Close()
	a, err := (&Client{BaseURL: srv.URL}).Get(context.Background(), "/quests", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Page) == 0 {
		t.Error("page dropped")
	}
}

func TestGetErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		header string
		check  func(error) bool
	}{
		{http.StatusNotFound, "", func(err error) bool { return errors.Is(err, ErrNotFound) }},
		{http.StatusTooManyRequests, "30", func(err error) bool {
			var rl *RateLimitError
			return errors.As(err, &rl) && rl.RetryAfter == 30*time.Second && strings.Contains(err.Error(), "30s")
		}},
		{http.StatusBadGateway, "", func(err error) bool {
			var he *HTTPError
			return errors.As(err, &he) && he.Status == http.StatusBadGateway
		}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.header != "" {
				w.Header().Set("Retry-After", tc.header)
			}
			w.WriteHeader(tc.status)
			w.Write(fixture(t, "missing.json"))
		}))
		_, err := (&Client{BaseURL: srv.URL}).Get(context.Background(), "/dungeons/nope", nil)
		srv.Close()
		if !tc.check(err) {
			t.Errorf("status %d: err = %v", tc.status, err)
		}
	}
}

func TestGetRejectsAnswerWithoutData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>maintenance</html>`))
	}))
	defer srv.Close()
	if _, err := (&Client{BaseURL: srv.URL}).Get(context.Background(), "/zones", nil); err == nil {
		t.Error("want an error for a non-JSON answer")
	}
}

// TestLiveDungeon calls the real API.
func TestLiveDungeon(t *testing.T) {
	if testing.Short() {
		t.Skip("calls forevercompanion.io")
	}
	if os.Getenv("WOWMCP_LIVE") == "" {
		t.Skip("set WOWMCP_LIVE=1 to call the real Forever Companion API")
	}
	a, err := (&Client{}).Get(context.Background(), "/dungeons/the-deadmines", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(a.Data), "The Deadmines") {
		t.Errorf("data = %.200s", a.Data)
	}
}
