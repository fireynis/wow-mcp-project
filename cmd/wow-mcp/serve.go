package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/blizzard"
	"wowapp/internal/config"
	"wowapp/internal/foreverdb"
	"wowapp/internal/keychain"
	"wowapp/internal/tools"
)

const foreverDBBaseURL = "https://foreverdb.net/api/v1"

// configPath and secrets are what serve reads its settings and keys from;
// tests replace them.
var (
	configPath = config.DefaultPath
	secrets    = func() keychain.Keychain {
		return keychain.Keychain{Runner: keychain.ExecRunner{}, Path: keychain.SecurityPath}
	}
)

// newForeverDB builds the ForeverDB client. The key is read from the Keychain
// on every call, so a missing key fails the game tools with a message that
// names the fix instead of stopping the server from starting.
func newForeverDB(cfg config.Config, kc keychain.Keychain) *foreverdb.Client {
	return &foreverdb.Client{
		BaseURL: foreverDBBaseURL,
		Cache:   &foreverdb.Cache{Dir: cfg.CacheDir + "/foreverdb"},
		Key: func(ctx context.Context) (string, error) {
			return kc.Get(ctx, "foreverdb")
		},
	}
}

// nopWriteCloser lets the protocol writer outlive the server.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// cmdServe runs the MCP server over stdin/stdout until the client disconnects.
// Stdout carries the protocol; diagnostics go to stderr only.
func cmdServe(e *env, args []string) error {
	if len(args) != 0 {
		fmt.Fprint(e.stderr, usage)
		return errUsage
	}
	cfg, err := config.Load(configPath())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fdb := newForeverDB(cfg, secrets())
	done := make(chan struct{})
	defer func() { stop(); <-done }() // end the check before the stderr writer goes away
	go func() {
		defer close(done)
		// A failed check only means the cache may be stale or the key is not set yet.
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := fdb.CheckMeta(checkCtx); err != nil {
			fmt.Fprintln(e.stderr, "wow-mcp: ForeverDB build check:", err)
		}
	}()
	transport := &mcp.IOTransport{Reader: io.NopCloser(e.stdin), Writer: nopWriteCloser{e.stdout}}
	auth := newAuth()
	chars := &blizzard.Profile{
		Client: &blizzard.Client{BaseURL: probeBase(cfg.Region), Auth: auth, HTTP: auth.HTTP},
		Region: cfg.Region,
	}
	deps := tools.Deps{
		Game:       fdb,
		Chars:      chars,
		Characters: append([]config.Character{cfg.Main}, cfg.Others...),
	}
	return tools.New(deps).Run(ctx, transport)
}
