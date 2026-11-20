package blizzard

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wowapp/internal/keychain"
)

type fakeKeys struct {
	mu sync.Mutex
	m  map[string]string
}

func newKeys(kv ...string) *fakeKeys {
	f := &fakeKeys{m: map[string]string{}}
	for i := 0; i < len(kv); i += 2 {
		f.m[kv[i]] = kv[i+1]
	}
	return f
}

func (f *fakeKeys) Get(_ context.Context, account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[account]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeys) Set(_ context.Context, account, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[account] = secret
	return nil
}

const (
	testID     = "client-id-123"
	testSecret = "client-secret-xyz"
)

func credKeys() *fakeKeys {
	return newKeys(keychain.BlizzardClientID, testID, keychain.BlizzardClientSecret, testSecret)
}

func TestAppTokenSendsBasicAuth(t *testing.T) {
	var gotAuth, gotForm, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotForm = string(b)
		w.Write([]byte(`{"access_token":"app-tok","expires_in":86399}`))
	}))
	defer srv.Close()
	a := &Auth{TokenURL: srv.URL, Keys: credKeys()}
	tok, err := a.AppToken(context.Background())
	if err != nil || tok != "app-tok" {
		t.Fatalf("AppToken = %q, %v", tok, err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testID+":"+testSecret))
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotForm != "grant_type=client_credentials" {
		t.Errorf("form = %q", gotForm)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotCT)
	}
}

func TestAppTokenCachedUntilNearExpiry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Write([]byte(`{"access_token":"tok` + strconv.Itoa(int(n)) + `","expires_in":3600}`))
	}))
	defer srv.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &Auth{TokenURL: srv.URL, Keys: credKeys(), Now: func() time.Time { return now }}
	ctx := context.Background()
	get := func() string {
		t.Helper()
		tok, err := a.AppToken(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	if get() != "tok1" {
		t.Fatal("first token")
	}
	now = now.Add(3600*time.Second - 61*time.Second) // 61 s left: still cached
	if get() != "tok1" || calls.Load() != 1 {
		t.Fatalf("expected the cached token, calls = %d", calls.Load())
	}
	now = now.Add(2 * time.Second) // 59 s left: refresh
	if get() != "tok2" || calls.Load() != 2 {
		t.Fatalf("expected a refresh, calls = %d", calls.Load())
	}
}

func TestAppTokenMissingKeyNamesFix(t *testing.T) {
	a := &Auth{TokenURL: "http://unused.invalid", Keys: newKeys()}
	_, err := a.AppToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "wow-mcp keys set blizzard-client-id") || !errors.Is(err, keychain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAppTokenErrorsScrubSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`bad client ` + testSecret))
	}))
	defer srv.Close()
	a := &Auth{TokenURL: srv.URL, Keys: credKeys()}
	_, err := a.AppToken(context.Background())
	if err == nil || strings.Contains(err.Error(), testSecret) {
		t.Fatalf("err = %v", err)
	}
}

func TestAppTokenCancelKeepsContextError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &Auth{TokenURL: srv.URL, Keys: credKeys()}
	_, err := a.AppToken(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestUserTokenExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	keys := newKeys(keychain.BlizzardUserToken, "user-token-abc",
		keychain.BlizzardUserExpiry, strconv.FormatInt(now.Add(-time.Second).Unix(), 10))
	a := &Auth{Keys: keys, Now: func() time.Time { return now }}
	_, err := a.UserToken(context.Background())
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != "Your Battle.net login has expired; run wow-mcp login." {
		t.Errorf("copy = %q", err.Error())
	}
}

func TestUserTokenMissingAndValid(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &Auth{Keys: newKeys(), Now: func() time.Time { return now }}
	if _, err := a.UserToken(context.Background()); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("missing: err = %v", err)
	}
	a.Keys = newKeys(keychain.BlizzardUserToken, "user-token-abc",
		keychain.BlizzardUserExpiry, strconv.FormatInt(now.Add(time.Hour).Unix(), 10))
	tok, err := a.UserToken(context.Background())
	if err != nil || tok != "user-token-abc" {
		t.Fatalf("valid: %q, %v", tok, err)
	}
}

// loginRig is an Auth wired to a fake token server, a listener on a free
// port and an opener that plays the browser.
type loginRig struct {
	auth    *Auth
	keys    *fakeKeys
	tokenFm chan url.Values
	tokAuth chan string
}

func newLoginRig(t *testing.T, browser func(authURL *url.URL, callback string)) *loginRig {
	t.Helper()
	r := &loginRig{keys: credKeys(), tokenFm: make(chan url.Values, 1), tokAuth: make(chan string, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(b))
		r.tokenFm <- form
		r.tokAuth <- req.Header.Get("Authorization")
		w.Write([]byte(`{"access_token":"usertok12345","expires_in":86399}`))
	}))
	t.Cleanup(srv.Close)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.auth = &Auth{
		TokenURL:     srv.URL,
		AuthorizeURL: "https://auth.example/authorize",
		Keys:         r.keys,
		Now:          func() time.Time { return now },
		Listen:       func() (net.Listener, error) { return ln, nil },
		Open: func(_ context.Context, raw string) error {
			u, err := url.Parse(raw)
			if err != nil {
				return err
			}
			go browser(u, "http://"+ln.Addr().String()+"/callback")
			return nil
		},
		LoginTimeout: 5 * time.Second,
	}
	return r
}

func TestLoginStoresTokenAndExpiry(t *testing.T) {
	type seen struct {
		q    url.Values
		page string
	}
	browser := make(chan seen, 1)
	r := newLoginRig(t, func(u *url.URL, cb string) {
		s := seen{q: u.Query()}
		resp, err := http.Get(cb + "?code=thecode&state=" + u.Query().Get("state"))
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			s.page = string(b)
		}
		browser <- s
	})
	expires, err := r.auth.Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := r.auth.Now().Add(86399 * time.Second)
	if !expires.Equal(want) {
		t.Errorf("expires = %v, want %v", expires, want)
	}
	if got := r.keys.m[keychain.BlizzardUserToken]; got != "usertok12345" {
		t.Errorf("stored token = %q", got)
	}
	if got := r.keys.m[keychain.BlizzardUserExpiry]; got != strconv.FormatInt(want.Unix(), 10) {
		t.Errorf("stored expiry = %q", got)
	}
	var sn seen
	select {
	case sn = <-browser:
	case <-time.After(5 * time.Second):
		t.Fatal("browser never finished")
	}
	authQuery, page := sn.q, sn.page
	form := <-r.tokenFm
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "thecode" || form.Get("redirect_uri") != "http://localhost:47830/callback" {
		t.Errorf("token form = %v", form)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(testID+":"+testSecret))
	if got := <-r.tokAuth; got != wantAuth {
		t.Errorf("token Authorization = %q", got)
	}
	for k, v := range map[string]string{
		"response_type": "code", "client_id": testID, "scope": "wow.profile",
		"redirect_uri": "http://localhost:47830/callback",
	} {
		if authQuery.Get(k) != v {
			t.Errorf("authorize %s = %q, want %q", k, authQuery.Get(k), v)
		}
	}
	if len(authQuery.Get("state")) < 16 {
		t.Errorf("state too short: %q", authQuery.Get("state"))
	}
	if !strings.Contains(page, "close") {
		t.Errorf("browser page = %q", page)
	}
}

func TestLoginRejectsWrongState(t *testing.T) {
	r := newLoginRig(t, func(u *url.URL, cb string) {
		resp, err := http.Get(cb + "?code=thecode&state=forged")
		if err == nil {
			resp.Body.Close()
		}
	})
	_, err := r.auth.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := r.keys.m[keychain.BlizzardUserToken]; ok {
		t.Error("a token was stored")
	}
	select {
	case <-r.tokenFm:
		t.Error("the code was exchanged")
	default:
	}
}

func TestLoginReportsDenial(t *testing.T) {
	r := newLoginRig(t, func(u *url.URL, cb string) {
		resp, err := http.Get(cb + "?error=access_denied&state=" + u.Query().Get("state"))
		if err == nil {
			resp.Body.Close()
		}
	})
	_, err := r.auth.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginTimesOut(t *testing.T) {
	r := newLoginRig(t, func(*url.URL, string) {})
	r.auth.LoginTimeout = 50 * time.Millisecond
	_, err := r.auth.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no Battle.net redirect") {
		t.Fatalf("err = %v", err)
	}
}
