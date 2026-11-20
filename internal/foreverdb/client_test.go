package foreverdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wowapp/internal/keychain"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{
		BaseURL: srv.URL,
		Key:     func(context.Context) (string, error) { return "k123", nil },
		Cache:   &Cache{Dir: t.TempDir()},
	}
}

func TestGetSendsKeyHeader(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "k123" {
			t.Errorf("X-API-Key = %q", got)
		}
		if r.URL.Path != "/items/1" || r.URL.Query().Get("q") != "a b" {
			t.Errorf("url = %s", r.URL)
		}
		w.Write([]byte(`{"data":1}`))
	})
	got, err := c.Get(context.Background(), "/items/1", url.Values{"q": {"a b"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"data":1}` {
		t.Errorf("got %s", got)
	}
}

func TestGetSendsUserAgent(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "wow-mcp/0.1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Write([]byte(`{}`))
	})
	if _, err := c.Get(context.Background(), "/meta", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGet429RetriesOnceThenFails(t *testing.T) {
	var n atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	})
	start := time.Now()
	_, err := c.Get(context.Background(), "/items", nil)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *RateLimitError", err)
	}
	if n.Load() != 2 {
		t.Errorf("requests = %d, want 2", n.Load())
	}
	if time.Since(start) < time.Second {
		t.Errorf("did not wait Retry-After")
	}
}

func TestGet429ThenSuccess(t *testing.T) {
	var n atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGet429RetryOutsideBudget(t *testing.T) {
	var n atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	c.CallBudget = 2 * time.Second
	start := time.Now()
	_, err := c.Get(context.Background(), "/items", nil)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *RateLimitError", err)
	}
	if n.Load() != 1 {
		t.Errorf("requests = %d, want 1", n.Load())
	}
	if time.Since(start) > time.Second {
		t.Errorf("waited instead of failing at once")
	}
}

func TestGet429WaitHonoursContext(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(429)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Get(ctx, "/items", nil)
	if err == nil {
		t.Fatal("want error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("wait ignored ctx cancellation")
	}
}

func TestGet402Quota(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		w.Write([]byte(`{"code":"subscription_required"}`))
	})
	_, err := c.Get(context.Background(), "/items", nil)
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrQuota", err)
	}
	if !strings.Contains(err.Error(), "ForeverDB quota used up for today") {
		t.Errorf("message = %q", err)
	}
}

func TestErrorScrubsKey(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("boom key=" + r.Header.Get("X-API-Key") + " " + strings.Repeat("x", 500)))
	})
	_, err := c.Get(context.Background(), "/items", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "k123") {
		t.Errorf("key leaked: %v", err)
	}
	if len(he.Body) > 200 {
		t.Errorf("body len %d > 200", len(he.Body))
	}
}

func TestErrorScrubsKeySplitAtCut(t *testing.T) {
	// The key straddles the 200-byte cut; no fragment may survive.
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(strings.Repeat("x", 198) + "k123 tail"))
	})
	_, err := c.Get(context.Background(), "/items", nil)
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatal(err)
	}
	if strings.Contains(he.Body, "k1") || len(he.Body) > 200 {
		t.Errorf("body = %q", he.Body)
	}
}

func TestKeyErrorPropagates(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request made") })
	c.Key = func(context.Context) (string, error) { return "", keychain.ErrNotFound }
	_, err := c.Get(context.Background(), "/items", nil)
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestNoRedirectFollowed(t *testing.T) {
	var other atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1) }))
	defer target.Close()
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	})
	_, err := c.Get(context.Background(), "/items", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 302 {
		t.Fatalf("err = %v", err)
	}
	if other.Load() != 0 {
		t.Error("redirect was followed")
	}
}

func TestBodyCap(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("a", maxBody+10)))
	})
	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestCacheHit(t *testing.T) {
	var n atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Write([]byte(`{"data":"x"}`))
	})
	for i := 0; i < 2; i++ {
		got, err := c.Get(context.Background(), "/items/1", url.Values{"b": {"2"}, "a": {"1"}})
		if err != nil || string(got) != `{"data":"x"}` {
			t.Fatalf("got %s, %v", got, err)
		}
	}
	if n.Load() != 1 {
		t.Errorf("requests = %d, want 1", n.Load())
	}
}

func TestErrorsNotCached(t *testing.T) {
	var n atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{}`))
	})
	if _, err := c.Get(context.Background(), "/items", nil); err == nil {
		t.Fatal("want error")
	}
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatal(err)
	}
}

func TestCacheInvalidatesOnMetaHashChange(t *testing.T) {
	var meta atomic.Value
	meta.Store(`{"data":{"build":"1"}}`)
	var items atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meta" {
			w.Write([]byte(meta.Load().(string)))
			return
		}
		items.Add(1)
		w.Write([]byte(`{"data":[]}`))
	})
	ctx := context.Background()
	if err := c.CheckMeta(ctx); err != nil {
		t.Fatal(err)
	}
	c.Get(ctx, "/items", nil)
	c.Get(ctx, "/items", nil)
	if items.Load() != 1 {
		t.Fatalf("items requests = %d, want 1", items.Load())
	}
	if err := c.CheckMeta(ctx); err != nil { // unchanged: cache kept
		t.Fatal(err)
	}
	c.Get(ctx, "/items", nil)
	if items.Load() != 1 {
		t.Fatalf("cache dropped on unchanged meta")
	}
	meta.Store(`{"data":{"build":"2"}}`)
	if err := c.CheckMeta(ctx); err != nil {
		t.Fatal(err)
	}
	c.Get(ctx, "/items", nil)
	if items.Load() != 2 {
		t.Errorf("items requests = %d, want 2 after meta change", items.Load())
	}
}

func TestLiveMeta(t *testing.T) {
	if testing.Short() {
		t.Skip("hits the real ForeverDB API")
	}
	kc := keychain.Keychain{Runner: keychain.ExecRunner{}, Path: keychain.SecurityPath}
	ctx := context.Background()
	key, err := kc.Get(ctx, keychain.ForeverDB)
	if errors.Is(err, keychain.ErrNotFound) {
		t.Skip("no foreverdb key in the Keychain")
	}
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{
		BaseURL: "https://foreverdb.net/api/v1",
		Key:     func(context.Context) (string, error) { return key, nil },
		Cache:   &Cache{Dir: t.TempDir()},
	}
	got, err := c.Get(ctx, "/meta", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Error("empty /meta")
	}
}
