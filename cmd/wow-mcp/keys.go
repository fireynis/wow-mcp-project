package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"wowapp/internal/keychain"
)

// maxKeyInput is how much of standard input "keys set" reads.
const maxKeyInput = 4 << 10

// keyStorer is the part of the Keychain the keys command uses.
type keyStorer interface {
	Set(ctx context.Context, account, secret string) error
	Has(ctx context.Context, account string) (bool, error)
}

// keyStore is the login Keychain; tests replace it.
var keyStore = func() keyStorer {
	return keychain.Keychain{Path: keychain.SecurityPath, Runner: keychain.ExecRunner{}}
}

// cmdKeys handles "keys set <account>" and "keys list".
func cmdKeys(e *env, args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(e.stderr, "wow-mcp keys: missing subcommand (set or list)\n\n%s", usage)
		return errUsage
	}
	switch args[0] {
	case "set":
		if len(args) != 2 {
			fmt.Fprintf(e.stderr, "wow-mcp keys set: want exactly one account (one of %s)\n", strings.Join(keychain.Accounts, ", "))
			return errUsage
		}
		return keysSet(e, args[1])
	case "list":
		if len(args) != 1 {
			fmt.Fprintf(e.stderr, "wow-mcp keys list: unexpected argument %q\n", args[1])
			return errUsage
		}
		return keysList(e)
	}
	fmt.Fprintf(e.stderr, "wow-mcp keys: unknown subcommand %q\n\n%s", args[0], usage)
	return errUsage
}

func keysSet(e *env, account string) error {
	if !keychain.ValidAccount(account) {
		fmt.Fprintf(e.stderr, "wow-mcp keys set: unknown account %q (one of %s)\n", account, strings.Join(keychain.Accounts, ", "))
		return errUsage
	}
	input, err := io.ReadAll(io.LimitReader(e.stdin, maxKeyInput))
	if err != nil {
		return fmt.Errorf("reading standard input: %w", err)
	}
	secret := strings.TrimSpace(string(input))
	if secret == "" {
		return errors.New("no key on standard input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := keyStore().Set(ctx, account, secret); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Stored the %s key in the login Keychain (service %s).\n", account, keychain.Service)
	return nil
}

func keysList(e *env) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := keyStore()
	for _, account := range keychain.Accounts {
		has, err := store.Has(ctx, account)
		if err != nil {
			return fmt.Errorf("%s: %w", account, err)
		}
		status := "missing"
		if has {
			status = "present"
		}
		fmt.Fprintf(e.stdout, "%s  %s\n", account, status)
	}
	return nil
}
