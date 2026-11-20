// Package config loads wow-mcp's config.toml. It holds no secrets; those live
// in the login Keychain.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Regions are the Blizzard API regions wow-mcp supports (KR is known to 403).
var Regions = []string{"us", "eu", "tw"}

// Character is a WoW Forever character. Forever has no realms: the API
// addresses a character by its name and surname ("Leafy" "Oakheart" ->
// /profile/wow/character/leafy-oakheart).
type Character struct {
	Name    string `toml:"name"`
	Surname string `toml:"surname"`
}

// Config is the loaded configuration.
type Config struct {
	Region          string
	Main            Character
	Others          []Character
	CacheDir        string
	StaticNamespace string
}

// file mirrors the TOML layout.
type file struct {
	Region          string      `toml:"region"`
	CacheDir        string      `toml:"cache_dir"`
	StaticNamespace string      `toml:"static_namespace"`
	Main            Character   `toml:"main"`
	Characters      []Character `toml:"characters"`
}

// DefaultPath is ~/.config/wow-app/config.toml.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "wow-app", "config.toml")
	}
	return filepath.Join(home, ".config", "wow-app", "config.toml")
}

// Load reads and validates the config at path. A missing required value or an
// unknown key is an error naming it.
func Load(path string) (Config, error) {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("config %s: unknown key %s", path, strings.Join(keys, ", "))
	}
	if !validRegion(f.Region) {
		return Config{}, fmt.Errorf("config %s: region must be one of %s (got %q)", path, strings.Join(Regions, ", "), f.Region)
	}
	if f.Main.Name == "" || f.Main.Surname == "" {
		return Config{}, fmt.Errorf("config %s: [main] needs both name and surname", path)
	}
	for i, c := range f.Characters {
		if c.Name == "" || c.Surname == "" {
			return Config{}, fmt.Errorf("config %s: [[characters]] entry %d needs both name and surname", path, i+1)
		}
	}
	cacheDir := f.CacheDir
	if strings.HasPrefix(cacheDir, "~/") || cacheDir == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("config %s: cache_dir %q needs a home directory: %w", path, cacheDir, err)
		}
		cacheDir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(cacheDir, "~"), "/"))
	}
	if cacheDir != "" && !filepath.IsAbs(cacheDir) {
		return Config{}, fmt.Errorf("config %s: cache_dir must be an absolute path or start with ~/ (got %q)", path, f.CacheDir)
	}
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("config %s: no cache_dir and no home directory: %w", path, err)
		}
		cacheDir = filepath.Join(home, "Library", "Caches", "wow-app")
	}
	return Config{
		Region:          f.Region,
		Main:            f.Main,
		Others:          f.Characters,
		CacheDir:        cacheDir,
		StaticNamespace: f.StaticNamespace,
	}, nil
}

func validRegion(r string) bool {
	for _, v := range Regions {
		if v == r {
			return true
		}
	}
	return false
}
