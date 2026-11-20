package tools

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestServerListsNoToolsWithNoDeps(t *testing.T) {
	cs := connect(t, New(Deps{}))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 0 {
		t.Errorf("tools = %d, want 0", len(res.Tools))
	}
	info := cs.InitializeResult().ServerInfo
	if info.Name != "wow" || info.Version != "0.1.0" {
		t.Errorf("server info = %+v", info)
	}
}

func TestErrorResult(t *testing.T) {
	r := errorResult("boom")
	if !r.IsError || len(r.Content) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if tc, ok := r.Content[0].(*mcp.TextContent); !ok || tc.Text != "boom" {
		t.Errorf("content = %+v", r.Content[0])
	}
}
