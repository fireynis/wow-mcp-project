// Package foreverdb is a read-only client for the ForeverDB API with an
// on-disk cache.
package foreverdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	userAgent         = "wow-mcp/0.1.0"
	defaultCallBudget = 15 * time.Second
	maxBody           = 4 << 20
	maxErrorBody      = 200
)

// ErrQuota means ForeverDB's daily quota is spent (HTTP 402).
var ErrQuota = errors.New("ForeverDB quota used up for today")

// RateLimitError is a 429 that was not, or could not be, retried away.
type RateLimitError struct {
	RetryAfter time.Duration // 0 when the answer gave none
	Retried    bool
}

func (e *RateLimitError) Error() string {
	switch {
	case e.Retried:
		return "ForeverDB rate limit hit again after waiting; try again shortly"
	case e.RetryAfter > 0:
		return fmt.Sprintf("ForeverDB rate limit hit; it asks for %s, which is more than this call may wait", e.RetryAfter)
	}
	return "ForeverDB rate limit hit; try again shortly"
}

// HTTPError is any other non-2xx answer. Body is at most 200 bytes with the
// API key scrubbed.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("ForeverDB: HTTP %d: %s", e.Status, e.Body)
}

// Client reads from ForeverDB. Key is called per request, so a key stored
// after start-up is picked up; its error (keychain.ErrNotFound when the key
// is missing) is returned wrapped.
type Client struct {
	BaseURL    string
	HTTP       *http.Client
	Key        func(ctx context.Context) (string, error)
	Cache      *Cache        // nil: no caching
	CallBudget time.Duration // default 15s: the longest one Get may take, waits included
}

// Get returns the raw JSON answer for path (for example "/items/19019") and
// query, from the cache when a fresh entry exists. Only 2xx answers are
// cached. A 429 is retried once, after Retry-After, when that wait fits in
// what is left of CallBudget.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	var ck string
	if c.Cache != nil {
		ck = cacheKey(path, query)
		if b, ok := c.Cache.get(ck); ok {
			return b, nil
		}
	}
	body, err := c.fetch(ctx, path, query)
	if err != nil {
		return nil, err
	}
	if c.Cache != nil {
		// A cache that cannot be written costs speed, not correctness.
		_ = c.Cache.put(ck, body)
	}
	return body, nil
}

// CheckMeta fetches /meta, bypassing the cache, and hashes the whole body as
// the build hash (the field that names the build is undocumented, so any
// change in /meta counts). When it differs from the stored one the cache is
// invalidated and the new hash stored. The first run, with no stored hash,
// invalidates too.
func (c *Client) CheckMeta(ctx context.Context) error {
	if c.Cache == nil {
		return nil
	}
	body, err := c.fetch(ctx, "/meta", nil)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	h := hex.EncodeToString(sum[:])
	if c.Cache.metaHash() == h {
		return nil
	}
	if err := c.Cache.Invalidate(); err != nil {
		return fmt.Errorf("ForeverDB cache: %w", err)
	}
	if err := c.Cache.setMetaHash(h); err != nil {
		return fmt.Errorf("ForeverDB cache: %w", err)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, path string, query url.Values) ([]byte, error) {
	key, err := c.Key(ctx)
	if err != nil {
		return nil, fmt.Errorf("ForeverDB key: %w", err)
	}
	budget := c.CallBudget
	if budget <= 0 {
		budget = defaultCallBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	deadline, _ := ctx.Deadline()

	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	body, retryAfter, err := c.do(ctx, u, key)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		return body, err
	}
	// 429: one retry, only when the wait fits in the budget that is left.
	if retryAfter <= 0 || retryAfter >= time.Until(deadline) {
		return nil, &RateLimitError{RetryAfter: retryAfter}
	}
	t := time.NewTimer(retryAfter)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("ForeverDB: waiting out the rate limit: %w", ctx.Err())
	case <-t.C:
	}
	body, _, err = c.do(ctx, u, key)
	if errors.As(err, &rl) {
		return nil, &RateLimitError{Retried: true}
	}
	return body, err
}

// do makes one request. A 429 comes back as a *RateLimitError along with the
// Retry-After it carried (0 when absent or unreadable).
func (c *Client) do(ctx context.Context, u, key string) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("ForeverDB: %s", scrub(err.Error(), key))
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	// A copy, so the caller's client is left as it was. A redirect is not
	// followed: it would send the key (a custom header Go does not strip) to
	// another host, so it is a non-2xx answer like any other.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("ForeverDB: %s", scrub(err.Error(), key))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusPaymentRequired:
		return nil, 0, ErrQuota
	case resp.StatusCode == http.StatusTooManyRequests:
		var wait time.Duration
		if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		return nil, wait, &RateLimitError{RetryAfter: wait}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		// Read enough that a key starting within the first maxErrorBody
		// bytes is whole when it is scrubbed, then cut.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxErrorBody+len(key))))
		msg := scrub(string(b), key)
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		msg = strings.TrimSpace(strings.ToValidUTF8(msg, ""))
		return nil, 0, &HTTPError{Status: resp.StatusCode, Body: msg}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, 0, fmt.Errorf("ForeverDB: reading the answer: %s", scrub(err.Error(), key))
	}
	if len(b) > maxBody {
		return nil, 0, fmt.Errorf("ForeverDB: answer too large (over %d bytes); narrow the query", maxBody)
	}
	return b, 0, nil
}

func scrub(msg, key string) string {
	if key == "" {
		return msg
	}
	return strings.ReplaceAll(msg, key, "[key]")
}
