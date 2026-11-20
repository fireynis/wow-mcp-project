package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"wowapp/internal/blizzard"
	"wowapp/internal/keychain"
)

// mapRunner answers security's find-generic-password from a map of accounts,
// so doctor's Keychain reads need no real Keychain.
type mapRunner map[string]string

func (m mapRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, int, error) {
	var account string
	withSecret := false
	for i, a := range args {
		if a == "-a" && i+1 < len(args) {
			account = args[i+1]
		}
		if a == "-w" {
			withSecret = true
		}
	}
	v, ok := m[account]
	if !ok {
		return nil, nil, 44, nil
	}
	if !withSecret {
		return nil, nil, 0, nil
	}
	return []byte(v + "\n"), nil, 0, nil
}

type doctorOpts struct {
	omit        string        // account left out of the Keychain
	userExpires time.Duration // user token lifetime from now
	charStatus  int           // answer for the character path
	fdbKey      string        // ForeverDB key in the Keychain (default: the one the server accepts)
}

// useDoctor wires doctor to a fake Blizzard and ForeverDB server and a fake
// Keychain.
func useDoctor(t *testing.T, o doctorOpts) {
	t.Helper()
	useTestServe(t)
	if o.userExpires == 0 {
		o.userExpires = 24 * time.Hour
	}
	if o.charStatus == 0 {
		o.charStatus = http.StatusOK
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Write([]byte(`{"access_token":"app-token-AAA111","expires_in":3600}`))
		case r.URL.Path == "/meta":
			if r.Header.Get("X-API-Key") != "fdbkey12345" {
				w.WriteHeader(http.StatusUnauthorized)
			}
			w.Write([]byte(`{"data":{},"meta":{}}`))
		case r.URL.Path == "/companion/dungeons":
			w.Write([]byte(`{"data":[],"meta":{}}`))
		case strings.HasPrefix(r.URL.Path, "/profile/wow/character/"):
			w.WriteHeader(o.charStatus)
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	keys := map[string]string{
		keychain.BlizzardClientID:     "cid",
		keychain.BlizzardClientSecret: "client-secret-CCC333",
		keychain.ForeverDB:            "fdbkey12345",
		keychain.BlizzardUserToken:    "user-token-BBB222",
		keychain.BlizzardUserExpiry:   strconv.FormatInt(time.Now().Add(o.userExpires).Unix(), 10),
	}
	if o.fdbKey != "" {
		keys[keychain.ForeverDB] = o.fdbKey
	}
	delete(keys, o.omit)
	kc := keychain.Keychain{Runner: mapRunner(keys), Path: keychain.SecurityPath}
	oldSecrets, oldAuth, oldBase, oldFDB, oldFC := secrets, newAuth, probeBase, doctorForeverDBBase, doctorCompanionBase
	doctorCompanionBase = srv.URL + "/companion"
	secrets = func() keychain.Keychain { return kc }
	newAuth = func() *blizzard.Auth {
		return &blizzard.Auth{TokenURL: srv.URL + "/token", Keys: kc, HTTP: srv.Client()}
	}
	probeBase = func(string) string { return srv.URL }
	doctorForeverDBBase = srv.URL
	t.Cleanup(func() {
		secrets, newAuth, probeBase, doctorForeverDBBase, doctorCompanionBase = oldSecrets, oldAuth, oldBase, oldFDB, oldFC
	})
}

func runDoctor(t *testing.T) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runIO([]string{"doctor"}, strings.NewReader(""), &out, &errb)
	return code, out.String() + errb.String()
}

func TestDoctorReportsMissingForeverDBKey(t *testing.T) {
	useDoctor(t, doctorOpts{omit: keychain.ForeverDB})
	code, out := runDoctor(t)
	if code != 1 {
		t.Errorf("exit %d, want 1\n%s", code, out)
	}
	found := false
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "FAIL") && strings.Contains(l, "wow-mcp keys set foreverdb") {
			found = true
		}
	}
	if !found {
		t.Errorf("no FAIL line naming the fix:\n%s", out)
	}
}

func TestDoctorAllGood(t *testing.T) {
	useDoctor(t, doctorOpts{})
	code, out := runDoctor(t)
	if code != 0 || strings.Contains(out, "FAIL") || strings.Contains(out, "warn") {
		t.Errorf("exit %d\n%s", code, out)
	}
	for _, secret := range []string{"fdbkey12345", "user-token-BBB222", "client-secret-CCC333", "app-token-AAA111"} {
		if strings.Contains(out, secret) {
			t.Errorf("output leaks %q", secret)
		}
	}
}

func TestDoctorWarnsWhenLoginExpiresSoon(t *testing.T) {
	useDoctor(t, doctorOpts{userExpires: time.Hour})
	code, out := runDoctor(t)
	if code != 0 || !strings.Contains(out, "warn") || !strings.Contains(out, "wow-mcp login") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestDoctorExpiredLoginIsAWarning(t *testing.T) {
	useDoctor(t, doctorOpts{userExpires: -time.Hour})
	code, out := runDoctor(t)
	if code != 0 || !strings.Contains(out, "warn  Battle.net login: Your Battle.net login has expired; run wow-mcp login.") {
		t.Errorf("exit %d\n%s", code, out)
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("expired login must not FAIL:\n%s", out)
	}
}

func TestDoctorNoLoginWarnsOnceAndExitsZero(t *testing.T) {
	for _, omit := range []string{keychain.BlizzardUserToken, keychain.BlizzardUserExpiry} {
		useDoctor(t, doctorOpts{omit: omit})
		code, out := runDoctor(t)
		if code != 0 || strings.Contains(out, "FAIL") {
			t.Errorf("omit %s: exit %d\n%s", omit, code, out)
		}
		warns := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "warn") && strings.Contains(l, "wow-mcp login") {
				warns++
			}
		}
		if warns != 1 {
			t.Errorf("omit %s: %d login warnings, want exactly 1\n%s", omit, warns, out)
		}
	}
}

func TestDoctorStatesWhichToolGroupsServeRegisters(t *testing.T) {
	useDoctor(t, doctorOpts{})
	_, out := runDoctor(t)
	for _, want := range []string{"game tools: registered", "character tools: registered", "Forever Companion tools: registered", "ok    Forever Companion: reachable"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorRejectedForeverDBKeyFails(t *testing.T) {
	useDoctor(t, doctorOpts{fdbKey: "wrong-key-999"})
	code, out := runDoctor(t)
	found := false
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "FAIL  ForeverDB") && strings.Contains(l, "wow-mcp keys set foreverdb") {
			found = true
		}
	}
	if code != 1 || !found || strings.Contains(out, "wrong-key-999") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestDoctorMainCharacterNotFound(t *testing.T) {
	useDoctor(t, doctorOpts{charStatus: http.StatusNotFound})
	code, out := runDoctor(t)
	if code != 1 || !strings.Contains(out, "check [main] name and surname") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestDoctorSkipsCharacterWithoutAppToken(t *testing.T) {
	useDoctor(t, doctorOpts{omit: keychain.BlizzardClientSecret})
	code, out := runDoctor(t)
	if code != 1 || !strings.Contains(out, "wow-mcp keys set blizzard-client-secret") || !strings.Contains(out, "skip") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestDoctorBadConfig(t *testing.T) {
	useDoctor(t, doctorOpts{})
	old := configPath
	configPath = func() string { return "/nonexistent/config.toml" }
	defer func() { configPath = old }()
	code, out := runDoctor(t)
	if code != 1 || !strings.Contains(out, "FAIL") || !strings.Contains(out, "config") {
		t.Errorf("exit %d\n%s", code, out)
	}
}
