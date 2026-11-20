package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"wowapp/internal/blizzard"
	"wowapp/internal/keychain"
)

const (
	probeAppToken  = "app-token-AAA111"
	probeUserToken = "user-token-BBB222"
	probeSecret    = "client-secret-CCC333"
)

type probeKeys map[string]string

func (k probeKeys) Get(_ context.Context, a string) (string, error) {
	if v, ok := k[a]; ok {
		return v, nil
	}
	return "", keychain.ErrNotFound
}
func (probeKeys) Set(context.Context, string, string) error { return nil }

// useProbeServer points probe at a fake that serves the token endpoint and
// echoes the bearer token, the client secret and the request line in every
// answer, so a leak shows up in the written files.
func useProbeServer(t *testing.T, loggedIn bool) {
	t.Helper()
	useTestServe(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"` + probeAppToken + `","expires_in":3600}`))
			return
		}
		if strings.Contains(r.URL.Path, "/realm/") {
			w.WriteHeader(http.StatusForbidden)
		}
		w.Write([]byte(`{"echo":"` + r.Header.Get("Authorization") + `","secret":"` + probeSecret + `","ns":"` + r.URL.Query().Get("namespace") + `"}`))
	}))
	t.Cleanup(srv.Close)
	keys := probeKeys{
		keychain.BlizzardClientID:     "cid",
		keychain.BlizzardClientSecret: probeSecret,
	}
	if loggedIn {
		keys[keychain.BlizzardUserToken] = probeUserToken
		keys[keychain.BlizzardUserExpiry] = strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	}
	oldAuth, oldBase := newAuth, probeBase
	newAuth = func() *blizzard.Auth { return &blizzard.Auth{TokenURL: srv.URL + "/token", Keys: keys} }
	probeBase = func(string) string { return srv.URL }
	t.Cleanup(func() { newAuth, probeBase = oldAuth, oldBase })
}

func TestProbeScrubsTokens(t *testing.T) {
	useProbeServer(t, true)
	dir := filepath.Join(t.TempDir(), "out")
	var out, errb bytes.Buffer
	if code := runIO([]string{"probe", "-out", dir}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) == 0 {
		t.Fatalf("no files: %v", err)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", st.Mode().Perm())
	}
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(dir, f.Name()))
		if info, _ := f.Info(); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f.Name(), info.Mode().Perm())
		}
		for _, s := range []string{probeAppToken, probeUserToken, probeSecret} {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%s contains %q", f.Name(), s)
			}
		}
	}
	for _, s := range []string{probeAppToken, probeUserToken, probeSecret} {
		if strings.Contains(out.String()+errb.String(), s) {
			t.Errorf("printed output contains %q", s)
		}
	}
	// The body was kept apart from the secrets.
	b, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if !strings.Contains(string(b), "Bearer") {
		t.Errorf("file lost its content: %s", b)
	}
}

func TestProbeTableListsEveryPath(t *testing.T) {
	useProbeServer(t, true)
	var out, errb bytes.Buffer
	if code := runIO([]string{"probe", "-out", filepath.Join(t.TempDir(), "o")}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, p := range []string{
		"/profile/user/wow",
		"/profile/wow/character/stormrage-forever/leafy",
		"/profile/wow/character/stormrage-forever/leafy/equipment",
		"/profile/wow/character/stormrage-forever/leafy/specializations",
		"/profile/wow/character/stormrage-forever/leafy/statistics",
		"/profile/wow/character/stormrage-forever/leafy/reputations",
		"/profile/wow/character/stormrage-forever/leafy/professions",
		"/profile/wow/character/stormrage-forever/leafy/character-media",
		"/data/wow/realm/index",
		"/data/wow/item/19019",
	} {
		if n := strings.Count(out.String(), p+" "); n < 2 {
			t.Errorf("%s listed %d times (want app and user rows):\n%s", p, n, out.String())
		}
	}
	for _, s := range []string{"profile-forever-us", "dynamic-forever-us", "static-forever-us", "app", "user", "403", "200"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("table lacks %q:\n%s", s, out.String())
		}
	}
}

func TestProbeSkipsUserRowsWhenLoggedOut(t *testing.T) {
	useProbeServer(t, false)
	var out, errb bytes.Buffer
	if code := runIO([]string{"probe", "-out", filepath.Join(t.TempDir(), "o")}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "skipped: not logged in") || !strings.Contains(out.String(), "200") {
		t.Errorf("table:\n%s", out.String())
	}
}

func TestProbeRejectsArguments(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runIO([]string{"probe", "extra"}, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("exit %d", code)
	}
}
