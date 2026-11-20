package foreverdb

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheKeySortsQuery(t *testing.T) {
	a := cacheKey("/items", url.Values{"a": {"1"}, "b": {"2", "3"}})
	b := cacheKey("/items", url.Values{"b": {"2", "3"}, "a": {"1"}})
	if a != b {
		t.Error("key depends on map order")
	}
	if a == cacheKey("/items", url.Values{"a": {"1"}}) || a == cacheKey("/zones", url.Values{"a": {"1"}, "b": {"2", "3"}}) {
		t.Error("distinct requests share a key")
	}
}

func TestCachePutGetAndTTL(t *testing.T) {
	c := &Cache{Dir: t.TempDir(), TTL: time.Hour}
	if _, ok := c.get("k"); ok {
		t.Fatal("hit on empty cache")
	}
	if err := c.put("k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if v, ok := c.get("k"); !ok || string(v) != "v" {
		t.Fatalf("got %q %v", v, ok)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(c.Dir, "k"), old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.get("k"); ok {
		t.Error("expired entry returned")
	}
}

func TestCacheDefaultTTL(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	c.put("k", []byte("v"))
	old := time.Now().Add(-23 * time.Hour)
	os.Chtimes(filepath.Join(c.Dir, "k"), old, old)
	if _, ok := c.get("k"); !ok {
		t.Error("23h-old entry expired under default 24h TTL")
	}
}

func TestInvalidateKeepsMetaHash(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	c.put("k", []byte("v"))
	if err := c.setMetaHash("h"); err != nil {
		t.Fatal(err)
	}
	if err := c.Invalidate(); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.get("k"); ok {
		t.Error("entry survived Invalidate")
	}
	if c.metaHash() != "h" {
		t.Error("Invalidate removed meta-hash")
	}
}
