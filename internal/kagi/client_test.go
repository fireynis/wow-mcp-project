package kagi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowapp/internal/keychain"
)

const testKey = "kagi-testkey-123456"

func keyFunc(k string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return k, nil }
}

func TestSearchRequestAndParse(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "kagi", "search.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	var auth, path, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path, method = r.Header.Get("Authorization"), r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Write(fixture)
	}))
	defer srv.Close()
	a, err := (&Client{BaseURL: srv.URL + "/api/v1", Key: keyFunc(testKey)}).Search(context.Background(), "WoW Forever druid leveling")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/v1/search" || auth != "Bearer "+testKey || got.Query != "WoW Forever druid leveling" || got.Limit != MaxResults {
		t.Errorf("request: %s %s auth %q body %+v", method, path, auth, got)
	}
	if len(a.Results) != 2 {
		t.Fatalf("results = %+v", a.Results)
	}
	if r := a.Results[0]; !r.AboutForever || r.Snippet != "Updated for the November patches." || r.Time != "2026-11-14T10:00:00Z" {
		t.Errorf("first = %+v", r)
	}
	if a.Results[1].AboutForever {
		t.Errorf("the Classic result is marked about Forever")
	}
	if strings.Join(a.Related, "|") != "wow forever druid talents|wow forever bear tank" {
		t.Errorf("related = %v", a.Related)
	}
}

func TestSearchErrorsScrubKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":[{"code":"auth","message":"bad token ` + testKey + `"}]}`))
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, Key: keyFunc(testKey)}).Search(context.Background(), "q")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusUnauthorized || strings.Contains(err.Error(), testKey) {
		t.Errorf("err = %v", err)
	}
}

func TestParseRejectsOddAnswers(t *testing.T) {
	for _, body := range []string{`<html>down</html>`, `{"meta":{}}`} {
		if _, err := parse([]byte(body)); err == nil {
			t.Errorf("%s: want an error", body)
		}
	}
	a, err := parse([]byte(`{"data":{}}`))
	if err != nil || a.Results == nil || len(a.Results) != 0 {
		t.Errorf("empty data: %+v, %v", a, err)
	}
}

func TestSearchKeyErrors(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Key: func(context.Context) (string, error) { return "", keychain.ErrNotFound }}
	if _, err := c.Search(context.Background(), "q"); !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	c.Key = keyFunc("")
	if _, err := c.Search(context.Background(), "q"); !errors.Is(err, ErrNoKey) {
		t.Errorf("err = %v", err)
	}
}

// TestLiveSearch calls the real API with the stored key (about $0.012).
func TestLiveSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("calls kagi.com")
	}
	if os.Getenv("WOWMCP_LIVE") == "" {
		t.Skip("set WOWMCP_LIVE=1 to call the real Kagi API")
	}
	kc := keychain.Keychain{Runner: keychain.ExecRunner{}, Path: keychain.SecurityPath}
	c := &Client{Key: func(ctx context.Context) (string, error) { return kc.Get(ctx, keychain.Kagi) }}
	a, err := c.Search(context.Background(), "WoW Forever druid leveling guide")
	if errors.Is(err, keychain.ErrNotFound) {
		t.Skip("no kagi key stored")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Results) == 0 {
		t.Errorf("no results: %+v", a)
	}
}
