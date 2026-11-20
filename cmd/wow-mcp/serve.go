package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/tools"
)

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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	transport := &mcp.IOTransport{Reader: io.NopCloser(e.stdin), Writer: nopWriteCloser{e.stdout}}
	return tools.New(tools.Deps{}).Run(ctx, transport)
}
