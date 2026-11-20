package blizzard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotFound is an HTTP 404 from the Blizzard API.
var ErrNotFound = errors.New("Blizzard API: not found")

// HTTPError is any other non-2xx answer. Body is at most 200 bytes with the
// token scrubbed.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Blizzard: HTTP %d: %s", e.Status, e.Body)
}

// APIBase is the API host for a region (us, eu, tw).
func APIBase(region string) string {
	return "https://" + region + ".api.blizzard.com"
}

// Client reads from the Blizzard API.
type Client struct {
	BaseURL string // APIBase(region)
	Auth    *Auth
	HTTP    *http.Client
}

// Get returns the raw JSON for path (for example "/profile/wow/character/x/y")
// in namespace. With user it sends the user token (ErrLoginExpired when there
// is none or Blizzard rejects it), otherwise the application token.
func (c *Client) Get(ctx context.Context, path, namespace string, user bool) (json.RawMessage, error) {
	var token string
	var err error
	if user {
		token, err = c.Auth.UserToken(ctx)
	} else {
		token, err = c.Auth.AppToken(ctx)
	}
	if err != nil {
		return nil, err
	}

	q := url.Values{"namespace": {namespace}, "locale": {"en_US"}}
	u := strings.TrimRight(c.BaseURL, "/") + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, scrubErr("Blizzard", err, token)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(c.HTTP).Do(req)
	if err != nil {
		return nil, scrubErr("Blizzard", err, token)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized && user:
		return nil, ErrLoginExpired
	case resp.StatusCode == http.StatusUnauthorized:
		c.Auth.forgetAppToken()
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &HTTPError{Status: resp.StatusCode, Body: readErrorBody(resp.Body, token)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, scrubErr("Blizzard: reading the answer", err, token)
	}
	if len(b) > maxBody {
		return nil, fmt.Errorf("Blizzard: answer too large (over %d bytes)", maxBody)
	}
	return b, nil
}
