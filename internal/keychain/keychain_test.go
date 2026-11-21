package keychain

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type call struct {
	path  string
	args  []string
	stdin string
}

type result struct {
	stdout, stderr string
	exit           int
	err            error
}

// fakeRunner records every call and returns the scripted results in order.
type fakeRunner struct {
	calls   []call
	results []result
}

func (f *fakeRunner) Run(_ context.Context, path string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	f.calls = append(f.calls, call{path, args, string(stdin)})
	if len(f.results) == 0 {
		return nil, nil, 0, errors.New("fakeRunner: no result scripted")
	}
	r := f.results[0]
	f.results = f.results[1:]
	return []byte(r.stdout), []byte(r.stderr), r.exit, r.err
}

func newKeychain(results ...result) (Keychain, *fakeRunner) {
	f := &fakeRunner{results: results}
	return Keychain{Path: SecurityPath, Runner: f}, f
}

func TestGet(t *testing.T) {
	cases := []struct {
		name    string
		res     result
		want    string
		wantErr func(error) bool
		errPart []string
		absent  []string
	}{
		{name: "found", res: result{stdout: "tok-abc12345\n"}, want: "tok-abc12345"},
		{name: "not found", res: result{exit: 44}, wantErr: func(err error) bool { return errors.Is(err, ErrNotFound) }},
		{name: "other failure", res: result{stdout: "tok-abc12345", stderr: "boom", exit: 1}, errPart: []string{"boom", "1"}, absent: []string{"tok-abc12345"}},
		{name: "could not run", res: result{err: errors.New("no such file")}, errPart: []string{"no such file"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, f := newKeychain(tc.res)
			got, err := k.Get(context.Background(), BlizzardClientID)
			if got != tc.want {
				t.Errorf("Get = %q, want %q", got, tc.want)
			}
			if tc.wantErr != nil && !tc.wantErr(err) {
				t.Errorf("err = %v", err)
			}
			if tc.wantErr == nil && tc.errPart == nil && err != nil {
				t.Errorf("err = %v", err)
			}
			for _, part := range tc.errPart {
				if err == nil || !strings.Contains(err.Error(), part) {
					t.Errorf("err = %v, want it to contain %q", err, part)
				}
			}
			for _, part := range tc.absent {
				if err != nil && strings.Contains(err.Error(), part) {
					t.Errorf("err = %v, must not contain %q", err, part)
				}
			}
			want := call{"/usr/bin/security", []string{"find-generic-password", "-s", "wow-app", "-a", "blizzard-client-id", "-w"}, ""}
			if len(f.calls) != 1 || f.calls[0].path != want.path || !slices.Equal(f.calls[0].args, want.args) || f.calls[0].stdin != "" {
				t.Errorf("calls = %+v, want one %+v", f.calls, want)
			}
		})
	}
}

func TestGetTrimsStderrInErrors(t *testing.T) {
	k, _ := newKeychain(result{stderr: strings.Repeat("x", 500), exit: 2})
	_, err := k.Get(context.Background(), BlizzardClientID)
	if err == nil || len(err.Error()) > 300 {
		t.Fatalf("err = %v, want a short error", err)
	}
}

func TestHas(t *testing.T) {
	cases := []struct {
		name string
		res  result
		want bool
	}{
		{"present", result{exit: 0}, true},
		{"missing", result{exit: 44}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, f := newKeychain(tc.res)
			got, err := k.Has(context.Background(), ForeverDB)
			if err != nil || got != tc.want {
				t.Fatalf("Has = %v, %v; want %v", got, err, tc.want)
			}
			want := []string{"find-generic-password", "-s", "wow-app", "-a", "foreverdb"}
			if len(f.calls) != 1 || !slices.Equal(f.calls[0].args, want) {
				t.Errorf("calls = %+v, want args %v", f.calls, want)
			}
		})
	}
	k, _ := newKeychain(result{exit: 1, stderr: "denied"})
	if _, err := k.Has(context.Background(), ForeverDB); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("Has with exit 1: err = %v, want one naming stderr", err)
	}
}

func TestSet(t *testing.T) {
	k, f := newKeychain(result{}, result{stdout: "tok-abc12345\n"})
	if err := k.Set(context.Background(), BlizzardClientID, "tok-abc12345"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls = %+v, want 2", f.calls)
	}
	first := f.calls[0]
	if first.path != "/usr/bin/security" || !slices.Equal(first.args, []string{"-i"}) {
		t.Errorf("first call = %+v, want /usr/bin/security -i", first)
	}
	if want := "add-generic-password -U -s wow-app -a blizzard-client-id -w tok-abc12345\n"; first.stdin != want {
		t.Errorf("stdin = %q, want %q", first.stdin, want)
	}
	for _, c := range f.calls {
		for _, a := range c.args {
			if strings.Contains(a, "tok-abc12345") {
				t.Errorf("argument %q contains the secret", a)
			}
		}
	}
}

func TestSetFailsWhenKeychainDidNotTakeIt(t *testing.T) {
	k, _ := newKeychain(result{}, result{stdout: "another-value\n"})
	err := k.Set(context.Background(), BlizzardClientID, "tok-abc12345")
	if err == nil || !strings.Contains(err.Error(), "the Keychain did not take the key") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "tok-abc12345") || strings.Contains(err.Error(), "another-value") {
		t.Errorf("err = %v leaks a secret", err)
	}
	k, _ = newKeychain(result{}, result{exit: 44})
	if err := k.Set(context.Background(), BlizzardClientID, "tok-abc12345"); err == nil {
		t.Error("Set succeeded though the key reads back as missing")
	}
}

func TestSetRefusesBeforeRunning(t *testing.T) {
	for _, secret := range []string{"", "short", "has space12", "quote\"12345", "-abcdefgh12", "new\nline1234", strings.Repeat("a", 513)} {
		k, f := newKeychain()
		if err := k.Set(context.Background(), BlizzardClientID, secret); err == nil {
			t.Errorf("Set(%q) succeeded", secret)
		} else if secret != "" && strings.Contains(err.Error(), secret) {
			t.Errorf("Set error %q contains the secret", err)
		}
		if len(f.calls) != 0 {
			t.Errorf("Set(%q) ran %+v", secret, f.calls)
		}
	}
}

func TestSetRedactsTheSecretFromSecurityOutput(t *testing.T) {
	k, _ := newKeychain(result{stderr: "unknown option tok-abc12345", exit: 1})
	err := k.Set(context.Background(), BlizzardClientID, "tok-abc12345")
	if err == nil || !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), "[key]") || strings.Contains(err.Error(), "tok-abc12345") {
		t.Fatalf("err = %v, want exit 1 and [key], without the secret", err)
	}
}

func TestBogusAccountRunsNothing(t *testing.T) {
	k, f := newKeychain()
	ctx := context.Background()
	if _, err := k.Get(ctx, "bogus"); err == nil {
		t.Error("Get accepted bogus")
	}
	if _, err := k.Has(ctx, "bogus"); err == nil {
		t.Error("Has accepted bogus")
	}
	if err := k.Set(ctx, "bogus", "tok-abc12345"); err == nil {
		t.Error("Set accepted bogus")
	}
	if len(f.calls) != 0 {
		t.Errorf("ran %+v", f.calls)
	}
}

func TestValidAccount(t *testing.T) {
	for account, want := range map[string]bool{"blizzard-client-id": true, "blizzard-client-secret": true, "foreverdb": true, "perplexity": true, "kagi": true, "blizzard-user-token": true, "blizzard-user-token-expiry": true, "bogus": false, "": false, "BlizzardClientID": false} {
		if got := ValidAccount(account); got != want {
			t.Errorf("ValidAccount(%q) = %v, want %v", account, got, want)
		}
	}
}

func TestAccountsAreTheSeven(t *testing.T) {
	want := []string{"blizzard-client-id", "blizzard-client-secret", "foreverdb", "perplexity", "kagi", "blizzard-user-token", "blizzard-user-token-expiry"}
	if !slices.Equal(Accounts, want) {
		t.Errorf("Accounts = %v, want %v", Accounts, want)
	}
	if Service != "wow-app" {
		t.Errorf("Service = %q", Service)
	}
}

func TestExecRunner(t *testing.T) {
	stdout, stderr, code, err := ExecRunner{}.Run(context.Background(), "/bin/sh", []string{"-c", "cat; echo err >&2; exit 3"}, []byte("hi"))
	if err != nil || string(stdout) != "hi" || string(stderr) != "err\n" || code != 3 {
		t.Errorf("got %q, %q, %d, %v; want \"hi\", \"err\\n\", 3, nil", stdout, stderr, code, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, _, err := (ExecRunner{}).Run(ctx, "/bin/sh", []string{"-c", "sleep 5"}, nil); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("a killed process: err = %v, want one saying timed out", err)
	}
	if _, _, _, err := (ExecRunner{}).Run(context.Background(), "/nonexistent/binary", nil, nil); err == nil {
		t.Error("a missing binary gave no error")
	}
}
