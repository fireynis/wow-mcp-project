package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"wowapp/internal/blizzard"
	"wowapp/internal/config"
	"wowapp/internal/foreverdb"
	"wowapp/internal/keychain"
)

// doctorForeverDBBase is the ForeverDB API doctor checks; tests replace it.
var doctorForeverDBBase = foreverDBBaseURL

// loginWarnWithin is how close to expiry the user token earns a warning.
const loginWarnWithin = 2 * time.Hour

// doctorHTTPTimeout bounds every request doctor makes.
const doctorHTTPTimeout = 30 * time.Second

// report prints one line per check and remembers whether any failed.
type report struct {
	e      *env
	failed bool
}

func (r *report) ok(name, detail string) { fmt.Fprintf(r.e.stdout, "ok    %s: %s\n", name, detail) }
func (r *report) warn(name, detail string) {
	fmt.Fprintf(r.e.stdout, "warn  %s: %s\n", name, detail)
}
func (r *report) skip(name, why string) { fmt.Fprintf(r.e.stdout, "skip  %s: %s\n", name, why) }
func (r *report) fail(name, problem, fix string) {
	r.failed = true
	if fix != "" {
		problem += " Fix: " + fix
	}
	fmt.Fprintf(r.e.stdout, "FAIL  %s: %s\n", name, problem)
}

// cmdDoctor checks everything wow-mcp needs and prints one line per check. It
// exits 1 when any check fails.
func cmdDoctor(e *env, args []string) error {
	if len(args) != 0 {
		fmt.Fprint(e.stderr, usage)
		return errUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	r := &report{e: e}

	path := configPath()
	cfg, cfgErr := config.Load(path)
	if cfgErr != nil {
		r.fail("config", cfgErr.Error(), "create or correct "+path+" (see the README)")
	} else {
		r.ok("config", "loaded "+path)
	}

	kc := secrets()
	have := map[string]bool{}
	for _, account := range keychain.Accounts {
		name := "keychain " + account
		has, err := kc.Has(ctx, account)
		switch {
		case err != nil:
			r.fail(name, err.Error(), "")
		case has:
			have[account] = true
			r.ok(name, "present")
		case account == keychain.BlizzardUserToken || account == keychain.BlizzardUserExpiry:
			r.fail(name, "missing.", "run wow-mcp login")
		default:
			r.fail(name, "missing.", "run wow-mcp keys set "+account)
		}
	}

	auth := newAuth()
	appOK := false
	if _, err := auth.AppToken(ctx); err != nil {
		r.fail("Blizzard app token", err.Error(), "check the client id and secret with wow-mcp keys list")
	} else {
		appOK = true
		r.ok("Blizzard app token", "obtained")
	}

	doctorUserToken(ctx, r, auth)
	doctorForeverDB(ctx, r, kc, have[keychain.ForeverDB])

	switch {
	case cfgErr != nil:
		r.skip("main character", "no config")
	case !appOK:
		r.skip("main character", "no app token")
	default:
		client := &blizzard.Client{BaseURL: probeBase(cfg.Region), Auth: auth, HTTP: auth.HTTP}
		status, _, err := client.Raw(ctx, blizzard.CharPath(cfg.Main), "profile-forever-"+cfg.Region, false)
		name := fmt.Sprintf("main character %s on %s", cfg.Main.Name, cfg.Main.Realm)
		switch {
		case err != nil:
			r.fail(name, err.Error(), "")
		case status >= 200 && status < 300:
			r.ok(name, "found")
		case status == http.StatusNotFound:
			r.fail(name, "not found (Blizzard's Forever character lookup may not work yet; see docs/blizzard-forever-api.md once probe has run).", "")
		default:
			r.fail(name, fmt.Sprintf("Blizzard answered %d %s.", status, http.StatusText(status)), "")
		}
	}

	if r.failed {
		return errors.New("some checks failed")
	}
	return nil
}

// doctorUserToken checks the stored Battle.net login and how long it lasts.
func doctorUserToken(ctx context.Context, r *report, auth *blizzard.Auth) {
	const name = "Battle.net login"
	if _, err := auth.UserToken(ctx); err != nil {
		r.fail(name, err.Error(), "")
		return
	}
	exp, err := auth.Keys.Get(ctx, keychain.BlizzardUserExpiry)
	secs, perr := strconv.ParseInt(strings.TrimSpace(exp), 10, 64)
	if err != nil || perr != nil {
		r.fail(name, "the stored expiry is unreadable.", "run wow-mcp login")
		return
	}
	expires := time.Unix(secs, 0)
	left := time.Until(expires).Round(time.Minute)
	if left < loginWarnWithin {
		r.warn(name, fmt.Sprintf("expires in %s; run wow-mcp login soon.", left))
		return
	}
	r.ok(name, "valid until "+expires.Local().Format("Mon 2 Jan 15:04 MST"))
}

// doctorForeverDB checks that /meta answers with the stored key, bypassing
// the cache.
func doctorForeverDB(ctx context.Context, r *report, kc keychain.Keychain, hasKey bool) {
	const name = "ForeverDB"
	if !hasKey {
		r.skip(name, "no key stored")
		return
	}
	c := &foreverdb.Client{
		BaseURL: doctorForeverDBBase,
		HTTP:    &http.Client{Timeout: doctorHTTPTimeout},
		Key:     func(ctx context.Context) (string, error) { return kc.Get(ctx, keychain.ForeverDB) },
	}
	if _, err := c.Get(ctx, "/meta", nil); err != nil {
		r.fail(name, err.Error(), "check the key with wow-mcp keys set foreverdb")
		return
	}
	r.ok(name, "/meta reachable with the stored key")
}
