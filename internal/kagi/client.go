// Package kagi runs web searches through the Kagi Search API
// (https://help.kagi.com/kagi/api/search.html): POST /api/v1/search with a
// Bearer API key, billed per request.
package kagi

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
	BaseURL = "https://kagi.com/api/v1"
	// MaxResults is how many web results one search returns.
	MaxResults = 10

	maxBody = 4 << 20
)

// ErrNoKey means no API key is stored.
var ErrNoKey = errors.New("no Kagi API key is stored")

// HTTPError is a non-2xx answer; Body is at most 200 bytes, with the key scrubbed.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Kagi: HTTP %d: %s", e.Status, e.Body)
}

// Result is one web result. AboutForever reports whether its title or
// snippet mentions Forever, a hint that it is about WoW Forever and not
// another version of the game.
type Result struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	Snippet      string `json:"snippet,omitempty"`
	Time         string `json:"time,omitempty"`
	AboutForever bool   `json:"aboutForever"`
}

// Answer is one search's results and Kagi's related searches.
type Answer struct {
	Results []Result `json:"results"`
	Related []string `json:"related,omitempty"`
}

// Client searches through Kagi.
type Client struct {
	BaseURL string // empty means BaseURL
	HTTP    *http.Client
	// Key returns the API key; it is read on every call.
	Key func(ctx context.Context) (string, error)
}

// Search runs query and returns up to MaxResults web results.
func (c *Client) Search(ctx context.Context, query string) (Answer, error) {
	key, err := c.Key(ctx)
	if err != nil {
		return Answer{}, err
	}
	if key == "" {
		return Answer{}, ErrNoKey
	}
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	body, err := json.Marshal(struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{query, MaxResults})
	if err != nil {
		return Answer{}, fmt.Errorf("Kagi: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/search", bytes.NewReader(body))
	if err != nil {
		return Answer{}, fmt.Errorf("Kagi: %s", scrub(err.Error(), key))
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Answer{}, fmt.Errorf("Kagi: %s", scrub(err.Error(), key))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := scrub(strings.TrimSpace(string(b)), key)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return Answer{}, &HTTPError{Status: resp.StatusCode, Body: msg}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Answer{}, fmt.Errorf("Kagi: reading the answer: %s", scrub(err.Error(), key))
	}
	if len(b) > maxBody {
		return Answer{}, fmt.Errorf("Kagi: answer too large (over %d bytes)", maxBody)
	}
	return parse(b)
}

func parse(b []byte) (Answer, error) {
	var reply struct {
		Data *struct {
			Search []struct {
				URL     string `json:"url"`
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
				Time    string `json:"time"`
			} `json:"search"`
			RelatedSearch []json.RawMessage `json:"related_search"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &reply); err != nil || reply.Data == nil {
		return Answer{}, errors.New("Kagi: unexpected answer")
	}
	a := Answer{Results: []Result{}}
	for _, r := range reply.Data.Search {
		if r.URL == "" || len(a.Results) == MaxResults {
			continue
		}
		snippet := strings.Join(strings.Fields(r.Snippet), " ")
		about := strings.Contains(strings.ToLower(r.Title+" "+snippet), "forever")
		a.Results = append(a.Results, Result{Title: r.Title, URL: r.URL, Snippet: snippet, Time: r.Time, AboutForever: about})
	}
	for _, raw := range reply.Data.RelatedSearch {
		if s := relatedText(raw); s != "" {
			a.Related = append(a.Related, s)
		}
	}
	return a, nil
}

// relatedText reads one related search, which the reference does not pin
// down: a plain string, or an object with a query, title or text field.
func relatedText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var o map[string]any
	if json.Unmarshal(raw, &o) != nil {
		return ""
	}
	for _, k := range []string{"query", "title", "text"} {
		if v, ok := o[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func scrub(msg, key string) string {
	if key == "" {
		return msg
	}
	return strings.ReplaceAll(msg, key, "[key]")
}
