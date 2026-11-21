package perplexity

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

const testKey = "pplx-testkey-123456"

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "perplexity", "answer.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func keyFunc(k string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return k, nil }
}

func TestSearchSendsForeverPromptAndParses(t *testing.T) {
	var got struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Write(fixture(t))
	}))
	defer srv.Close()
	res, err := (&Client{BaseURL: srv.URL, Key: keyFunc(testKey)}).Search(context.Background(), "WoW Forever bear form health")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/chat/completions" || auth != "Bearer "+testKey || got.Model != DefaultModel {
		t.Errorf("request: path %q auth %q model %q", path, auth, got.Model)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || !strings.Contains(got.Messages[0].Content, "Classic Era") || got.Messages[1].Content != "WoW Forever bear form health" {
		t.Errorf("messages = %+v", got.Messages)
	}
	if !strings.HasPrefix(res.Answer, "In WoW Forever") {
		t.Errorf("answer = %q", res.Answer)
	}
	if len(res.Sources) != 2 || res.Sources[0].Title != "WoW Forever Druid changes" || res.Sources[0].Date != "2026-11-13" {
		t.Errorf("sources = %+v", res.Sources)
	}
}

func TestSearchFallsBackToCitations(t *testing.T) {
	r, err := parse([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"}}],"citations":["https://a.example","https://b.example"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sources) != 2 || r.Sources[1].URL != "https://b.example" {
		t.Errorf("sources = %+v", r.Sources)
	}
}

func TestSearchErrorsScrubKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key ` + testKey + `"}`))
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, Key: keyFunc(testKey)}).Search(context.Background(), "q")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks the key: %v", err)
	}
}

func TestSearchRejectsEmptyAnswer(t *testing.T) {
	for _, body := range []string{`<html>oops</html>`, `{"choices":[]}`, `{"choices":[{"message":{"content":"  "}}]}`} {
		if _, err := parse([]byte(body)); err == nil {
			t.Errorf("%s: want an error", body)
		}
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

// TestLiveSearch calls the real API with the stored key.
func TestLiveSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("calls api.perplexity.ai")
	}
	if os.Getenv("WOWMCP_LIVE") == "" {
		t.Skip("set WOWMCP_LIVE=1 to call the real Perplexity API")
	}
	kc := keychain.Keychain{Runner: keychain.ExecRunner{}, Path: keychain.SecurityPath}
	c := &Client{Key: func(ctx context.Context) (string, error) { return kc.Get(ctx, keychain.Perplexity) }}
	res, err := c.Search(context.Background(), "What changed for Druids in recent WoW Forever patches?")
	if errors.Is(err, keychain.ErrNotFound) {
		t.Skip("no perplexity key stored")
	}
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer == "" || len(res.Sources) == 0 {
		t.Errorf("result = %+v", res)
	}
}
