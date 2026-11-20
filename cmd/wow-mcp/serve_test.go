package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowapp/internal/config"
	"wowapp/internal/keychain"
)

type notFoundRunner struct{ calls int }

func (r *notFoundRunner) Run(context.Context, string, []string, []byte) ([]byte, []byte, int, error) {
	r.calls++
	return nil, nil, 44, nil
}

// useTestServe points serve at a valid temporary config and a Keychain that
// has no items, so no test touches the real login Keychain.
func useTestServe(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "region = \"us\"\ncache_dir = \"" + t.TempDir() + "\"\n[main]\nname = \"Leafy\"\nsurname = \"Oakheart\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCfg, oldSecrets := configPath, secrets
	configPath = func() string { return path }
	secrets = func() keychain.Keychain {
		return keychain.Keychain{Runner: &notFoundRunner{}, Path: keychain.SecurityPath}
	}
	t.Cleanup(func() { configPath, secrets = oldCfg, oldSecrets })
}

func TestServeBadConfigFails(t *testing.T) {
	old := configPath
	configPath = func() string { return filepath.Join(t.TempDir(), "missing.toml") }
	defer func() { configPath = old }()
	var out, errb bytes.Buffer
	code := runIO([]string{"serve"}, strings.NewReader(""), &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "config") {
		t.Errorf("code = %d stderr = %q", code, errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestForeverDBKeyIsReadLazilyFromKeychain(t *testing.T) {
	r := &notFoundRunner{}
	cfg := config.Config{CacheDir: t.TempDir()}
	c := newForeverDB(cfg, keychain.Keychain{Runner: r, Path: keychain.SecurityPath})
	if r.calls != 0 {
		t.Fatalf("Keychain read at construction: %d", r.calls)
	}
	_, err := c.Get(context.Background(), "/items", nil)
	if !errors.Is(err, keychain.ErrNotFound) || r.calls != 1 {
		t.Errorf("err = %v, calls = %d", err, r.calls)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.CacheDir, "foreverdb")); statErr != nil && !os.IsNotExist(statErr) {
		t.Error(statErr)
	}
}
