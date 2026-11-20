package blizzard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"wowapp/internal/keychain"
)

func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"app-tok-9","expires_in":86399}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(t *testing.T, api *httptest.Server, keys *fakeKeys) *Client {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &Client{
		BaseURL: api.URL,
		Auth:    &Auth{TokenURL: tokenServer(t).URL, Keys: keys, Now: func() time.Time { return now }},
	}
}

func TestAPIBase(t *testing.T) {
	if got := APIBase("eu"); got != "https://eu.api.blizzard.com" {
		t.Errorf("APIBase = %q", got)
	}
}

func TestGetAddsNamespaceAndLocale(t *testing.T) {
	var gotQuery, gotAuth, gotPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotAuth, gotPath = r.URL.RawQuery, r.Header.Get("Authorization"), r.URL.EscapedPath()
		w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()
	c := clientFor(t, api, credKeys())
	b, err := c.Get(context.Background(), "/profile/wow/character/argent-dawn/%C3%A6lfwine", "profile-classic-us", false)
	if err != nil || string(b) != `{"ok":true}` {
		t.Fatalf("Get = %s, %v", b, err)
	}
	if gotQuery != "locale=en_US&namespace=profile-classic-us" {
		t.Errorf("query = %q", gotQuery)
	}
	if gotPath != "/profile/wow/character/argent-dawn/%C3%A6lfwine" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer app-tok-9" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if strings.Contains(gotQuery, "app-tok") {
		t.Error("token in the query")
	}
}

func TestUser401IsLoginExpired(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("token user-tok-777 rejected"))
	}))
	defer api.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	keys := newKeys(keychain.BlizzardUserToken, "user-tok-777",
		keychain.BlizzardUserExpiry, strconv.FormatInt(now.Add(time.Hour).Unix(), 10))
	c := clientFor(t, api, keys)
	_, err := c.Get(context.Background(), "/x", "profile-classic-us", true)
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err = %v", err)
	}
}

func TestUserCallWithExpiredTokenNeverCallsAPI(t *testing.T) {
	called := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer api.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	keys := newKeys(keychain.BlizzardUserToken, "user-tok-777",
		keychain.BlizzardUserExpiry, strconv.FormatInt(now.Add(-time.Minute).Unix(), 10))
	c := clientFor(t, api, keys)
	_, err := c.Get(context.Background(), "/x", "ns", true)
	if !errors.Is(err, ErrLoginExpired) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestGet404AndOtherStatus(t *testing.T) {
	status := 404
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte("echo app-tok-9 body"))
	}))
	defer api.Close()
	c := clientFor(t, api, credKeys())
	if _, err := c.Get(context.Background(), "/x", "ns", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404: err = %v", err)
	}
	status = 500
	_, err := c.Get(context.Background(), "/x", "ns", false)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 || strings.Contains(err.Error(), "app-tok-9") {
		t.Fatalf("500: err = %v", err)
	}
}

func TestGetTransportErrorKeepsContextAndScrubs(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer api.Close()
	c := clientFor(t, api, credKeys())
	// Get the app token first so only the API call sees the cancelled context.
	if _, err := c.Auth.AppToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Get(ctx, "/x", "ns", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestRawReturnsFailureBodies(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"detail":"nope"}`))
	}))
	defer api.Close()
	c := clientFor(t, api, credKeys())
	status, body, err := c.Raw(context.Background(), "/x", "ns", false)
	if err != nil || status != 403 || string(body) != `{"detail":"nope"}` {
		t.Fatalf("status=%d body=%q err=%v", status, body, err)
	}
}
