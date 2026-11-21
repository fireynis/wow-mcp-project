package tools

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/kagi"
	"wowapp/internal/keychain"
)

// KagiSearcher runs a Kagi web search.
type KagiSearcher interface {
	Search(ctx context.Context, query string) (kagi.Answer, error)
}

// kagiResult is what kagi_search returns.
type kagiResult struct {
	Source  string        `json:"source"`
	Note    string        `json:"note"`
	Results []kagi.Result `json:"results"`
	Related []string      `json:"related,omitempty"`
}

const kagiNote = "aboutForever marks results whose title or snippet mentions Forever; the others probably describe Classic Era or another version. Read a page with WebFetch before relying on it."

func kagiError(err error) string {
	var he *kagi.HTTPError
	switch {
	case errors.Is(err, keychain.ErrNotFound), errors.Is(err, kagi.ErrNoKey):
		return "No Kagi API key is stored; run `wow-mcp keys set kagi` in a terminal. Use perplexity_search or WebSearch instead for now."
	case errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden):
		return "Kagi rejected the API key (HTTP " + strconv.Itoa(he.Status) + "); store a new one with `wow-mcp keys set kagi`."
	case errors.As(err, &he) && he.Status == http.StatusPaymentRequired:
		return "Kagi says the API account is out of credit (HTTP 402); top it up at kagi.com/api."
	case errors.As(err, &he) && he.Status == http.StatusTooManyRequests:
		return "Kagi rate limit reached; try again shortly or use perplexity_search."
	}
	return err.Error()
}

func registerKagi(srv *mcp.Server, s KagiSearcher) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "kagi_search",
		Description: "Web search through Kagi: up to 10 result pages (title, URL, snippet, date), each marked aboutForever when it mentions Forever, " +
			"plus related searches. Use it to find specific guides, patch notes or threads, or when perplexity_search comes up thin; " +
			"read a result with WebFetch. Put \"WoW Forever\" in the query. Source: Kagi (kagi.com).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
		q := strings.TrimSpace(in.Query)
		if q == "" {
			return errorResult("query is empty"), nil, nil
		}
		a, err := s.Search(ctx, q)
		if err != nil {
			return errorResult(kagiError(err)), nil, nil
		}
		return textResult(kagiResult{Source: "https://kagi.com", Note: kagiNote, Results: a.Results, Related: a.Related}), nil, nil
	})
}
