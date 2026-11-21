// Package perplexity runs web searches through Perplexity's chat completions
// API (https://docs.perplexity.ai), told to prefer WoW Forever sources over
// Classic Era and the other versions of the game.
package perplexity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// BaseURL is the API root.
	BaseURL = "https://api.perplexity.ai"
	// DefaultModel is the search model used when Client.Model is empty.
	DefaultModel = "sonar"

	maxTokens = 700
	maxBody   = 2 << 20
)

// SystemPrompt keeps the search on WoW Forever.
const SystemPrompt = `You search the web for a player of World of Warcraft: Forever (WoW Forever). WoW Forever is Blizzard's own version of World of Warcraft: it started from Classic Era and has since changed a lot, with new zones, dungeons, talents, items, quests and patch changes. Classic Era, Season of Discovery, Hardcore, Anniversary realms, Cataclysm Classic, Mists Classic and retail are different games, and private servers are not Forever.

Prefer sources that are explicitly about WoW Forever (they say "Forever" or "WoW Forever"). Use a source about another version only when nothing Forever-specific answers the question, and then say which version it describes and that Forever may differ. Never present Classic Era numbers, drops or talents as Forever facts. If you find nothing about Forever, say so.

Answer with plain facts in a few short paragraphs. Give dates for patch changes when the sources have them.`

// ErrNoKey means no API key is stored.
var ErrNoKey = errors.New("no Perplexity API key is stored")

// HTTPError is a non-2xx answer; Body is at most 200 bytes, with the key scrubbed.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Perplexity: HTTP %d: %s", e.Status, e.Body)
}

// Source is one web page the answer drew on.
type Source struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
	Date  string `json:"date,omitempty"`
}

// Result is one search answer and its sources.
type Result struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources,omitempty"`
}

// Client searches through Perplexity.
type Client struct {
	BaseURL string // empty means BaseURL
	Model   string // empty means DefaultModel
	HTTP    *http.Client
	// Key returns the API key; it is read on every call so a key stored
	// while the server runs is picked up.
	Key func(ctx context.Context) (string, error)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Search asks query and returns the answer with its sources.
func (c *Client) Search(ctx context.Context, query string) (Result, error) {
	key, err := c.Key(ctx)
	if err != nil {
		return Result{}, err
	}
	if key == "" {
		return Result{}, ErrNoKey
	}
	base, model := c.BaseURL, c.Model
	if base == "" {
		base = BaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	body, err := json.Marshal(struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		Messages  []message `json:"messages"`
	}{model, maxTokens, []message{{"system", SystemPrompt}, {"user", query}}})
	if err != nil {
		return Result{}, fmt.Errorf("Perplexity: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("Perplexity: %s", scrub(err.Error(), key))
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 45 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("Perplexity: %s", scrub(err.Error(), key))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := scrub(strings.TrimSpace(string(b)), key)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return Result{}, &HTTPError{Status: resp.StatusCode, Body: msg}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Result{}, fmt.Errorf("Perplexity: reading the answer: %s", scrub(err.Error(), key))
	}
	if len(b) > maxBody {
		return Result{}, fmt.Errorf("Perplexity: answer too large (over %d bytes)", maxBody)
	}
	return parse(b)
}

func parse(b []byte) (Result, error) {
	var reply struct {
		Choices []struct {
			Message message `json:"message"`
		} `json:"choices"`
		Citations     []string `json:"citations"`
		SearchResults []Source `json:"search_results"`
	}
	if err := json.Unmarshal(b, &reply); err != nil {
		return Result{}, errors.New("Perplexity: the answer is not JSON")
	}
	if len(reply.Choices) == 0 || strings.TrimSpace(reply.Choices[0].Message.Content) == "" {
		return Result{}, errors.New("Perplexity: the answer is empty")
	}
	r := Result{Answer: strings.TrimSpace(reply.Choices[0].Message.Content), Sources: reply.SearchResults}
	if len(r.Sources) == 0 {
		for _, u := range reply.Citations {
			r.Sources = append(r.Sources, Source{URL: u})
		}
	}
	return r, nil
}

func scrub(msg, key string) string {
	if key == "" {
		return msg
	}
	return strings.ReplaceAll(msg, key, "[key]")
}
