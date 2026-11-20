package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

type fakeKeyStore struct {
	sets    [][2]string
	present map[string]bool
}

func (f *fakeKeyStore) Set(_ context.Context, account, secret string) error {
	f.sets = append(f.sets, [2]string{account, secret})
	return nil
}

func (f *fakeKeyStore) Has(_ context.Context, account string) (bool, error) {
	return f.present[account], nil
}

func runKeys(t *testing.T, store *fakeKeyStore, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	orig := keyStore
	keyStore = func() keyStorer { return store }
	t.Cleanup(func() { keyStore = orig })
	var out, errb bytes.Buffer
	code = runIO(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestKeysSetReadsStdin(t *testing.T) {
	store := &fakeKeyStore{}
	code, stdout, stderr := runKeys(t, store, "  abc12345xyz\n", "keys", "set", "foreverdb")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	if len(store.sets) != 1 || store.sets[0] != [2]string{"foreverdb", "abc12345xyz"} {
		t.Errorf("Set calls = %v", store.sets)
	}
	if want := "Stored the foreverdb key in the login Keychain (service wow-app).\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if strings.Contains(stdout+stderr, "abc12345xyz") {
		t.Error("output contains the key")
	}
}

func TestKeysSetUnknownAccount(t *testing.T) {
	store := &fakeKeyStore{}
	code, _, stderr := runKeys(t, store, "abc12345xyz\n", "keys", "set", "nope")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	for _, account := range []string{"blizzard-client-id", "blizzard-client-secret", "foreverdb", "blizzard-user-token", "blizzard-user-token-expiry"} {
		if !strings.Contains(stderr, account) {
			t.Errorf("stderr %q does not list %s", stderr, account)
		}
	}
	if len(store.sets) != 0 {
		t.Errorf("Set calls = %v", store.sets)
	}
}

func TestKeysSetEmptyStdin(t *testing.T) {
	store := &fakeKeyStore{}
	code, _, stderr := runKeys(t, store, " \n", "keys", "set", "foreverdb")
	if code != 1 || !strings.Contains(stderr, "no key on standard input") || len(store.sets) != 0 {
		t.Errorf("exit = %d, stderr %q, sets %v", code, stderr, store.sets)
	}
}

func TestKeysSetReadsAtMost4KiB(t *testing.T) {
	store := &fakeKeyStore{}
	runKeys(t, store, strings.Repeat("a", 10000), "keys", "set", "foreverdb")
	if len(store.sets) != 1 || len(store.sets[0][1]) != 4096 {
		t.Fatalf("Set calls = %v, want one of 4096 bytes", len(store.sets))
	}
}

func TestKeysList(t *testing.T) {
	store := &fakeKeyStore{present: map[string]bool{"foreverdb": true}}
	code, stdout, _ := runKeys(t, store, "", "keys", "list")
	want := "blizzard-client-id  missing\nblizzard-client-secret  missing\nforeverdb  present\nblizzard-user-token  missing\nblizzard-user-token-expiry  missing\n"
	if code != 0 || stdout != want {
		t.Errorf("exit = %d, stdout %q, want %q", code, stdout, want)
	}
}

func TestKeysUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"keys"}, {"keys", "frob"}, {"keys", "set"}, {"keys", "set", "foreverdb", "extra"}, {"keys", "list", "extra"}, {"bogus"}, {"serve", "extra"}} {
		if code, _, _ := runKeys(t, &fakeKeyStore{}, "abc12345xyz", args...); code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
	}
}

func TestNoArgsServes(t *testing.T) {
	useTestServe(t)
	// Empty stdin: the server sees EOF and exits cleanly, writing nothing to stdout.
	code, stdout, stderr := runKeys(t, &fakeKeyStore{}, "")
	if code != 0 || stdout != "" {
		t.Errorf("exit = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// At a terminal, "keys set" prompts on stderr and reads one hidden line, so
// pressing Enter finishes it instead of waiting for end-of-input.
func TestKeysSetPromptsAtTerminal(t *testing.T) {
	store := &fakeKeyStore{}
	origKS, origTerm, origRead := keyStore, isTerminal, readHidden
	keyStore = func() keyStorer { return store }
	isTerminal = func(int) bool { return true }
	readHidden = func(int) ([]byte, error) { return []byte(" client-id-123 "), nil }
	t.Cleanup(func() { keyStore, isTerminal, readHidden = origKS, origTerm, origRead })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	var out, errb bytes.Buffer
	if code := runIO([]string{"keys", "set", "blizzard-client-id"}, r, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb.String())
	}
	if len(store.sets) != 1 || store.sets[0] != [2]string{"blizzard-client-id", "client-id-123"} {
		t.Errorf("Set calls = %v", store.sets)
	}
	if want := "Paste the blizzard-client-id key and press Enter (input is hidden): \n"; errb.String() != want {
		t.Errorf("stderr = %q, want %q", errb.String(), want)
	}
}
