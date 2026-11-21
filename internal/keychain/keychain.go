// Package keychain reads and writes wow-mcp's secrets (API keys and the
// Battle.net user token) in the macOS login Keychain, through
// /usr/bin/security.
package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// SecurityPath is the macOS security tool; it is always run by absolute path.
const SecurityPath = "/usr/bin/security"

// Service is the Keychain service every key is stored under.
const Service = "wow-app"

// The accounts: the Blizzard client credentials, the ForeverDB key, the
// Perplexity key (optional: only perplexity_search needs it), and the
// Battle.net user token with its expiry (a Unix timestamp in seconds, so it
// is plain ASCII digits).
const (
	BlizzardClientID     = "blizzard-client-id"
	BlizzardClientSecret = "blizzard-client-secret"
	ForeverDB            = "foreverdb"
	Perplexity           = "perplexity"
	BlizzardUserToken    = "blizzard-user-token"
	BlizzardUserExpiry   = "blizzard-user-token-expiry"
)

// Accounts lists every account, in the order "keys list" shows them.
var Accounts = []string{BlizzardClientID, BlizzardClientSecret, ForeverDB, Perplexity, BlizzardUserToken, BlizzardUserExpiry}

// ErrNotFound means the account has no item in the login Keychain.
var ErrNotFound = errors.New("not in the login Keychain")

// callTimeout bounds each run of security.
const callTimeout = 5 * time.Second

// exitNotFound is security's exit status for "The specified item could not be
// found in the keychain."
const exitNotFound = 44

// maxStderr is how much of security's stderr an error carries.
const maxStderr = 200

// secretPattern is what Set accepts: characters that need no quoting in a
// security -i command line, starting with a character that cannot be read as
// an option.
var secretPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+/=-]{7,511}$`)

// Runner runs path with args and stdin. err is only for "could not run";
// a non-zero exit is exitCode with err nil.
type Runner interface {
	Run(ctx context.Context, path string, args []string, stdin []byte) (stdout, stderr []byte, exitCode int, err error)
}

// ExecRunner runs real processes.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, path string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && ctx.Err() != nil {
		return nil, nil, 0, fmt.Errorf("timed out: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}

// Keychain is the login Keychain, reached through Path (SecurityPath).
type Keychain struct {
	Path   string
	Runner Runner
}

// ValidAccount reports whether account is one of Accounts.
func ValidAccount(account string) bool {
	for _, a := range Accounts {
		if a == account {
			return true
		}
	}
	return false
}

// Get returns the secret stored for account. ErrNotFound means there is none.
func (k Keychain) Get(ctx context.Context, account string) (string, error) {
	stdout, err := k.find(ctx, account, true)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(stdout), "\n"), nil
}

// Has reports whether account has a secret, without reading it.
func (k Keychain) Has(ctx context.Context, account string) (bool, error) {
	_, err := k.find(ctx, account, false)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNotFound):
		return false, nil
	}
	return false, err
}

// Set stores secret for account, replacing any earlier one. It feeds
// "security -i" the add-generic-password command on standard input, so the
// secret is in neither argv nor ps; interactive mode does not reliably report
// a failed command in its exit status, so Set reads the key back.
func (k Keychain) Set(ctx context.Context, account, secret string) error {
	if !ValidAccount(account) {
		return fmt.Errorf("unknown account %q", account)
	}
	if !secretPattern.MatchString(secret) {
		return errors.New("a key must be 8 to 512 characters of letters, digits and . _ ~ + / = -")
	}
	line := "add-generic-password -U -s " + Service + " -a " + account + " -w " + secret + "\n"
	runCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, stderr, code, err := k.Runner.Run(runCtx, k.Path, []string{"-i"}, []byte(line))
	// security parsed a line holding the secret and may quote it back.
	redact := func(s string) string { return strings.ReplaceAll(s, secret, "[key]") }
	if err != nil {
		return errors.New("security: " + redact(err.Error()))
	}
	if code != 0 {
		return exitError(code, []byte(redact(string(stderr))))
	}
	got, err := k.Get(ctx, account)
	if err != nil {
		return fmt.Errorf("the Keychain did not take the key: %w", err)
	}
	if got != secret {
		return errors.New("the Keychain did not take the key")
	}
	return nil
}

// find runs find-generic-password for account; withSecret adds -w, which
// makes security print the secret.
func (k Keychain) find(ctx context.Context, account string, withSecret bool) ([]byte, error) {
	if !ValidAccount(account) {
		return nil, fmt.Errorf("unknown account %q", account)
	}
	args := []string{"find-generic-password", "-s", Service, "-a", account}
	if withSecret {
		args = append(args, "-w")
	}
	runCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	stdout, stderr, code, err := k.Runner.Run(runCtx, k.Path, args, nil)
	switch {
	case err != nil:
		return nil, fmt.Errorf("security: %w", err)
	case code == exitNotFound:
		return nil, ErrNotFound
	case code != 0:
		return nil, exitError(code, stderr)
	}
	return stdout, nil
}

// exitError names the exit code and the start of stderr; stdout, which may
// hold a secret, never appears.
func exitError(code int, stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	if len(msg) > maxStderr {
		msg = strings.TrimSpace(msg[:maxStderr])
	}
	return fmt.Errorf("security exited %d: %s", code, msg)
}
