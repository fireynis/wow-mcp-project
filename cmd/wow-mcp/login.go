package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"wowapp/internal/blizzard"
	"wowapp/internal/keychain"
)

// newAuth builds the Blizzard OAuth layer on the login Keychain; tests
// replace it.
var newAuth = func() *blizzard.Auth {
	return &blizzard.Auth{
		HTTP: &http.Client{Timeout: 30 * time.Second},
		Keys: keychain.Keychain{Path: keychain.SecurityPath, Runner: keychain.ExecRunner{}},
	}
}

// cmdLogin signs in to Battle.net in the browser and stores the user token.
func cmdLogin(e *env, args []string) error {
	if len(args) != 0 {
		fmt.Fprint(e.stderr, usage)
		return errUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Fprintln(e.stdout, "Opening Battle.net in your browser; waiting up to 5 minutes for you to sign in.")
	expires, err := newAuth().Login(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Logged in. The login expires %s.\n", expires.Local().Format("Mon 2 Jan 2006 15:04 MST"))
	return nil
}
