// Package forevercompanion reads the Forever Companion API
// (https://forevercompanion.io/developers): free, read-only WoW Forever data
// with no key. Limit: 120 requests per 60 seconds per IP. Answers must credit
// Forever Companion with a link back.
package forevercompanion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// BaseURL is the API root.
	BaseURL = "https://forevercompanion.io/api/v1"
	// Site is the link the credit points at.
	Site = "https://forevercompanion.io"
	// Attribution is the credit used when an answer carries none.
	Attribution = "Data from Forever Companion (https://forevercompanion.io). Fan-made, not affiliated with Blizzard Entertainment."

	userAgent = "wow-mcp/0.1.0 (+https://github.com/fireynis/wow-mcp-project)"
	maxBody   = 4 << 20
)

// ErrNotFound is an HTTP 404.
var ErrNotFound = errors.New("Forever Companion: not found")

// RateLimitError is an HTTP 429.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("Forever Companion rate limit reached (120 requests a minute); try again in %s", e.RetryAfter.Round(time.Second))
	}
	return "Forever Companion rate limit reached (120 requests a minute); try again in a minute"
}

// HTTPError is any other non-2xx answer; Body is at most 200 bytes.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Forever Companion: HTTP %d: %s", e.Status, e.Body)
}

// Answer is one API answer without its repeated meta block.
type Answer struct {
	Data        json.RawMessage `json:"data"`
	Page        json.RawMessage `json:"page,omitempty"`
	Attribution string          `json:"-"`
}

// Client reads from the API.
type Client struct {
	BaseURL string // empty means BaseURL
	HTTP    *http.Client
}

// Get fetches path (for example "/dungeons/the-deadmines") with query q.
func (c *Client) Get(ctx context.Context, path string, q url.Values) (Answer, error) {
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	u := strings.TrimRight(base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Answer{}, fmt.Errorf("Forever Companion: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Answer{}, fmt.Errorf("Forever Companion: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Answer{}, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return Answer{}, &RateLimitError{RetryAfter: time.Duration(secs) * time.Second}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return Answer{}, &HTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Answer{}, fmt.Errorf("Forever Companion: reading the answer: %w", err)
	}
	if len(b) > maxBody {
		return Answer{}, fmt.Errorf("Forever Companion: answer too large (over %d bytes)", maxBody)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
		Page json.RawMessage `json:"page"`
		Meta struct {
			Attribution string `json:"attribution"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(b, &env); err != nil || len(env.Data) == 0 {
		return Answer{}, fmt.Errorf("Forever Companion: unexpected answer for %s", path)
	}
	a := Answer{Data: env.Data, Page: env.Page, Attribution: env.Meta.Attribution}
	if a.Attribution == "" {
		a.Attribution = Attribution
	}
	return a, nil
}
