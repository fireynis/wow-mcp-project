package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/kagi"
	"wowapp/internal/keychain"
)

type fakeKagi struct {
	query string
	err   error
}

func (f *fakeKagi) Search(_ context.Context, q string) (kagi.Answer, error) {
	f.query = q
	if f.err != nil {
		return kagi.Answer{}, f.err
	}
	return kagi.Answer{Results: []kagi.Result{{Title: "WoW Forever Druid guide", URL: "https://example.com/g", AboutForever: true}}, Related: []string{"wow forever druid talents"}}, nil
}

func callKagi(t *testing.T, f *fakeKagi, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	cs := connect(t, New(Deps{Kagi: f}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "kagi_search", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Content[0].(*mcp.TextContent).Text
}

func TestKagiToolReturnsResults(t *testing.T) {
	f := &fakeKagi{}
	res, text := callKagi(t, f, map[string]any{"query": " WoW Forever druid guide "})
	if res.IsError || f.query != "WoW Forever druid guide" {
		t.Fatalf("query %q, result %s", f.query, text)
	}
	var out kagiResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != "https://kagi.com" || len(out.Results) != 1 || !out.Results[0].AboutForever || len(out.Related) != 1 || !strings.Contains(out.Note, "Classic Era") {
		t.Errorf("result = %s", text)
	}
}

func TestKagiToolErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{keychain.ErrNotFound, "wow-mcp keys set kagi"},
		{&kagi.HTTPError{Status: http.StatusUnauthorized}, "rejected the API key"},
		{&kagi.HTTPError{Status: http.StatusPaymentRequired}, "out of credit"},
		{&kagi.HTTPError{Status: http.StatusTooManyRequests}, "rate limit"},
	} {
		if res, text := callKagi(t, &fakeKagi{err: tc.err}, map[string]any{"query": "q"}); !res.IsError || !strings.Contains(text, tc.want) {
			t.Errorf("%v: result %q", tc.err, text)
		}
	}
	f := &fakeKagi{}
	if res, _ := callKagi(t, f, map[string]any{"query": ""}); !res.IsError || f.query != "" {
		t.Error("empty query should fail without a search")
	}
}
