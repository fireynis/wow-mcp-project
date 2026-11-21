package tools

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/keychain"
	"wowapp/internal/perplexity"
)

// WebSearcher runs a broad web search (Perplexity).
type WebSearcher interface {
	Search(ctx context.Context, query string) (perplexity.Result, error)
}

// searchResult is what perplexity_search returns.
type searchResult struct {
	Source  string              `json:"source"`
	Note    string              `json:"note"`
	Answer  string              `json:"answer"`
	Sources []perplexity.Source `json:"sources,omitempty"`
}

const searchNote = "Web results can describe Classic Era or other versions; trust a source for Forever only when it is about WoW Forever, and prefer the ForeverDB and Forever Companion tools where they cover the question."

type searchIn struct {
	Query string `json:"query" jsonschema:"the question to search the web for; name WoW Forever in it"`
}

func searchError(err error) string {
	var he *perplexity.HTTPError
	switch {
	case errors.Is(err, keychain.ErrNotFound), errors.Is(err, perplexity.ErrNoKey):
		return "No Perplexity API key is stored; run `wow-mcp keys set perplexity` in a terminal. Use WebSearch instead for now."
	case errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden):
		return "Perplexity rejected the API key (HTTP " + strconv.Itoa(he.Status) + "); store a new one with `wow-mcp keys set perplexity`."
	case errors.As(err, &he) && he.Status == http.StatusTooManyRequests:
		return "Perplexity rate limit reached; try again shortly or use WebSearch."
	}
	return err.Error()
}

func registerSearch(srv *mcp.Server, s WebSearcher) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "perplexity_search",
		Description: "Broad web search through Perplexity, steered to WoW Forever sources: guides, patch notes, community discussion, " +
			"anything the ForeverDB and Forever Companion tools do not cover. Returns a short answer with its source pages. " +
			"Put \"WoW Forever\" in the query. Source: Perplexity (perplexity.ai).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
		q := strings.TrimSpace(in.Query)
		if q == "" {
			return errorResult("query is empty"), nil, nil
		}
		res, err := s.Search(ctx, q)
		if err != nil {
			return errorResult(searchError(err)), nil, nil
		}
		return textResult(searchResult{Source: "https://www.perplexity.ai", Note: searchNote, Answer: res.Answer, Sources: res.Sources}), nil, nil
	})
}
