package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"wowapp/internal/blizzard"
	"wowapp/internal/config"
	"wowapp/internal/keychain"
)

// probeItemID is a well-known Classic item (Thunderfury) used to see whether
// the static namespace answers at all.
const probeItemID = "19019"

// probeBase is the API host for a region; tests replace it.
var probeBase = blizzard.APIBase

type probeRequest struct{ path, namespace string }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// cmdProbe asks Blizzard what the Forever profile API returns for the
// configured character, with the application token and, when logged in, the
// user token, and writes every answer to a directory with the tokens scrubbed.
func cmdProbe(e *env, args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outDir := fs.String("out", "", "directory for the raw answers (default: a new temporary directory)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprint(e.stderr, usage)
		return errUsage
	}
	cfg, err := config.Load(configPath())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	auth := newAuth()
	client := &blizzard.Client{BaseURL: probeBase(cfg.Region), Auth: auth, HTTP: auth.HTTP}

	// Tokens and the client secret are read up front so they can be scrubbed.
	// Nothing prints before this, so a failure here leaks nothing.
	appTok, err := auth.AppToken(ctx)
	if err != nil {
		return err
	}
	hide := []string{appTok}
	secret, err := auth.Keys.Get(ctx, keychain.BlizzardClientSecret)
	if err != nil {
		return fmt.Errorf("Blizzard client secret: %w", err)
	}
	hide = append(hide, secret)
	userTok, userErr := auth.UserToken(ctx)
	if userErr == nil {
		hide = append(hide, userTok)
	} else if !errors.Is(userErr, blizzard.ErrLoginExpired) {
		return fmt.Errorf("Battle.net login: %w", userErr)
	}
	scrub := func(s string) string {
		for _, h := range hide {
			if h != "" {
				s = strings.ReplaceAll(s, h, "[secret]")
			}
		}
		return s
	}

	dir := *outDir
	if dir == "" {
		if dir, err = os.MkdirTemp("", "wow-mcp-probe-"); err != nil {
			return err
		}
	} else if fi, statErr := os.Stat(dir); statErr == nil && fi.IsDir() {
		// An existing directory is the user's: leave its mode alone and write
		// into a fresh private subdirectory.
		if dir, err = os.MkdirTemp(dir, "probe-"); err != nil {
			return err
		}
	} else if statErr == nil {
		return fmt.Errorf("-out %s is not a directory", dir)
	} else {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}

	staticNS := cfg.StaticNamespace
	if staticNS == "" {
		staticNS = "static-forever-" + cfg.Region
	}
	profileNS := "profile-forever-" + cfg.Region
	char := blizzard.CharPath(cfg.Main)
	reqs := []probeRequest{
		{"/profile/user/wow", profileNS},
		{char, profileNS},
	}
	for _, sub := range []string{"equipment", "specializations", "statistics", "reputations", "professions", "character-media"} {
		reqs = append(reqs, probeRequest{char + "/" + sub, profileNS})
	}
	reqs = append(reqs,
		probeRequest{"/data/wow/item/" + probeItemID, staticNS},
	)

	fmt.Fprintf(e.stdout, "Writing answers to %s\n\n", dir)
	tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tNAMESPACE\tTOKEN\tSTATUS")
	for _, r := range reqs {
		for _, user := range []bool{false, true} {
			kind := "app"
			if user {
				kind = "user"
			}
			status := probeOne(ctx, client, dir, r, kind, user, userErr, scrub)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", scrub(r.path), r.namespace, kind, status)
		}
	}
	return tw.Flush()
}

// probeOne makes one request, writes its answer and returns the table's
// status text.
func probeOne(ctx context.Context, c *blizzard.Client, dir string, r probeRequest, kind string, user bool, userErr error, scrub func(string) string) string {
	if user && userErr != nil {
		return "skipped: not logged in"
	}
	status, body, err := c.Raw(ctx, r.path, r.namespace, user)
	if err != nil {
		if errors.Is(err, blizzard.ErrLoginExpired) {
			return "skipped: not logged in"
		}
		return "error: " + scrub(err.Error())
	}
	name := unsafeName.ReplaceAllString(strings.TrimPrefix(r.path, "/"), "_") + "." + kind + ".txt"
	content := fmt.Sprintf("GET %s\nnamespace: %s\ntoken: %s\nstatus: %d %s\n\n%s\n",
		r.path, r.namespace, kind, status, http.StatusText(status), body)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(scrub(content)), 0o600); err != nil {
		return fmt.Sprintf("%d (write failed: %s)", status, scrub(err.Error()))
	}
	return fmt.Sprint(status)
}
