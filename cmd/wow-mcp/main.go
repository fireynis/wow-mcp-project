// wow-mcp is a read-only MCP server for WoW Forever character and game data.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const usage = `Usage: wow-mcp [command]

Commands:
  serve                run the MCP server on stdin/stdout (the default)
  keys set <account>   store a secret, read from standard input, in the login Keychain
                       (blizzard-client-id, blizzard-client-secret, foreverdb,
                       blizzard-user-token, blizzard-user-token-expiry)
  keys list            say which secrets are stored (never their values)
`

var errUsage = errors.New("usage")

// env is what a command needs from its surroundings.
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
}

func main() {
	os.Exit(runIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	commands := map[string]func(*env, []string) error{
		"serve": cmdServe,
		"keys":  cmdKeys,
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "wow-mcp: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	err := cmd(&env{stdin: stdin, stdout: stdout, stderr: stderr}, args[1:])
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		return 2
	}
	fmt.Fprintln(stderr, "wow-mcp:", err)
	return 1
}
