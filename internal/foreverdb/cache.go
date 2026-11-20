package foreverdb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTTL   = 24 * time.Hour
	metaHashFile = "meta-hash"
)

// Cache is an on-disk cache of successful ForeverDB answers, one file per
// request under Dir, named by cacheKey. An entry's age is its file's
// modification time. The build hash from /meta lives in Dir/meta-hash.
type Cache struct {
	Dir string
	TTL time.Duration // default 24h
}

// cacheKey is the sha256 of the path and the query sorted by key (and the
// values of a key in the order given), so equal requests share an entry.
func cacheKey(path string, query url.Values) string {
	sum := sha256.Sum256([]byte(path + "?" + query.Encode()))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return defaultTTL
}

func (c *Cache) get(key string) ([]byte, bool) {
	p := filepath.Join(c.Dir, key)
	info, err := os.Stat(p)
	if err != nil || time.Since(info.ModTime()) > c.ttl() {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return b, true
}

// put writes through a temp file and a rename, so a reader never sees half
// an answer.
func (c *Cache) put(key string, body []byte) error {
	return c.writeFile(key, body)
}

func (c *Cache) writeFile(name string, body []byte) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(body)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(c.Dir, name)); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// Invalidate removes every cached answer. It keeps meta-hash.
func (c *Cache) Invalidate() error {
	entries, err := os.ReadDir(c.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() || e.Name() == metaHashFile {
			continue
		}
		if err := os.Remove(filepath.Join(c.Dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *Cache) metaHash() string {
	b, err := os.ReadFile(filepath.Join(c.Dir, metaHashFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (c *Cache) setMetaHash(h string) error {
	return c.writeFile(metaHashFile, []byte(h))
}
