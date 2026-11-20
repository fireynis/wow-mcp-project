package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMinimal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := Load(writeConfig(t, "region = \"us\"\n[main]\nrealm = \"stormrage-forever\"\nname = \"Leafy\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "us" || cfg.Main != (Character{Realm: "stormrage-forever", Name: "Leafy"}) {
		t.Errorf("cfg = %+v", cfg)
	}
	if want := filepath.Join(home, "Library", "Caches", "wow-app"); cfg.CacheDir != want {
		t.Errorf("CacheDir = %q, want %q", cfg.CacheDir, want)
	}
	if cfg.StaticNamespace != "" || len(cfg.Others) != 0 {
		t.Errorf("cfg = %+v, want no namespace and no others", cfg)
	}
}

func TestLoadFull(t *testing.T) {
	cfg, err := Load(writeConfig(t, `region = "eu"
cache_dir = "/var/cache/x"
static_namespace = "static-1.60.1_70399-forever-us"
[main]
realm = "a"
name = "B"
[[characters]]
realm = "c"
name = "D"
[[characters]]
realm = "e"
name = "F"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheDir != "/var/cache/x" || cfg.StaticNamespace != "static-1.60.1_70399-forever-us" || len(cfg.Others) != 2 || cfg.Others[1].Name != "F" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadUnknownKey(t *testing.T) {
	_, err := Load(writeConfig(t, "region = \"us\"\nregoin = \"x\"\n[main]\nrealm = \"a\"\nname = \"b\"\n"))
	if err == nil || !strings.Contains(err.Error(), "regoin") {
		t.Fatalf("err = %v, want it to name regoin", err)
	}
}

func TestLoadMissingMain(t *testing.T) {
	_, err := Load(writeConfig(t, "region = \"us\"\n"))
	if err == nil || !strings.Contains(err.Error(), "main") {
		t.Fatalf("err = %v, want it to name main", err)
	}
}

func TestLoadBadRegion(t *testing.T) {
	for _, region := range []string{"kr", ""} {
		_, err := Load(writeConfig(t, "region = \""+region+"\"\n[main]\nrealm = \"a\"\nname = \"b\"\n"))
		if err == nil || !strings.Contains(err.Error(), "us, eu, tw") || !strings.Contains(err.Error(), "region") {
			t.Errorf("region %q: err = %v, want it to name region and list us, eu, tw", region, err)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("a missing file loaded")
	}
}

func TestLoadIncompleteCharacter(t *testing.T) {
	_, err := Load(writeConfig(t, "region = \"us\"\n[main]\nrealm = \"a\"\nname = \"b\"\n[[characters]]\nrealm = \"c\"\n"))
	if err == nil || !strings.Contains(err.Error(), "characters") {
		t.Fatalf("err = %v, want it to name characters", err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	if got, want := DefaultPath(), "/home/x/.config/wow-app/config.toml"; got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestCacheDirTildeExpands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := Load(writeConfig(t, "region = \"us\"\ncache_dir = \"~/x\"\n[main]\nrealm = \"a\"\nname = \"b\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "x"); cfg.CacheDir != want {
		t.Errorf("CacheDir = %q, want %q", cfg.CacheDir, want)
	}
}

func TestCacheDirRelativeRejected(t *testing.T) {
	_, err := Load(writeConfig(t, "region = \"us\"\ncache_dir = \"rel/x\"\n[main]\nrealm = \"a\"\nname = \"b\"\n"))
	if err == nil || !strings.Contains(err.Error(), "cache_dir") {
		t.Fatalf("err = %v, want one naming cache_dir", err)
	}
}

func TestCacheDirDefaultIsAbsolute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load(writeConfig(t, "region = \"us\"\n[main]\nrealm = \"a\"\nname = \"b\"\n"))
	if err != nil || !filepath.IsAbs(cfg.CacheDir) {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
}
