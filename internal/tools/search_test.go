package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/keychain"
	"wowapp/internal/perplexity"
)

type fakeSearch struct {
	query string
	err   error
}

func (f *fakeSearch) Search(_ context.Context, q string) (perplexity.Result, error) {
	f.query = q
	if f.err != nil {
		return perplexity.Result{}, f.err
	}
	return perplexity.Result{Answer: "Bear Form adds 20 health in Forever.", Sources: []perplexity.Source{{Title: "Forever patch notes", URL: "https://example.com/p"}}}, nil
}

func callSearch(t *testing.T, f *fakeSearch, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	cs := connect(t, New(Deps{Search: f}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "perplexity_search", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Content[0].(*mcp.TextContent).Text
}

func TestSearchToolReturnsAnswerAndSources(t *testing.T) {
	f := &fakeSearch{}
	res, text := callSearch(t, f, map[string]any{"query": "  WoW Forever bear form  "})
	if res.IsError || f.query != "WoW Forever bear form" {
		t.Fatalf("query %q, result %s", f.query, text)
	}
	var out searchResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.Answer == "" || len(out.Sources) != 1 || !strings.Contains(out.Note, "Classic Era") {
		t.Errorf("result = %s", text)
	}
}

func TestSearchToolErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{keychain.ErrNotFound, "wow-mcp keys set perplexity"},
		{&perplexity.HTTPError{Status: http.StatusUnauthorized}, "rejected the API key"},
		{&perplexity.HTTPError{Status: http.StatusTooManyRequests}, "rate limit"},
	} {
		if res, text := callSearch(t, &fakeSearch{err: tc.err}, map[string]any{"query": "q"}); !res.IsError || !strings.Contains(text, tc.want) {
			t.Errorf("%v: result %q", tc.err, text)
		}
	}
	f := &fakeSearch{}
	if res, _ := callSearch(t, f, map[string]any{"query": " "}); !res.IsError || f.query != "" {
		t.Error("empty query should fail without a search")
	}
}

func TestServerInstructionsSeparateForeverFromClassic(t *testing.T) {
	cs := connect(t, New(Deps{}))
	got := cs.InitializeResult().Instructions
	for _, want := range []string{"WoW Forever", "Classic Era", "perplexity_search"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions lack %q: %s", want, got)
		}
	}
}
