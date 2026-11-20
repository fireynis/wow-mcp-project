// Package blizzard is a read-only client for the Battle.net APIs: the OAuth
// layer (application token, user token, login) and a generic API client.
package blizzard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"wowapp/internal/keychain"
)

const (
	// TokenURL and AuthorizeURL are Battle.net's OAuth endpoints.
	TokenURL     = "https://oauth.battle.net/token"
	AuthorizeURL = "https://oauth.battle.net/authorize"

	// RedirectURI is where Battle.net sends the browser after login. It is
	// fixed: it must match what is registered with the Blizzard client.
	RedirectURI = "http://localhost:47830/callback"
	// ListenAddr is where login listens for that redirect.
	ListenAddr = "127.0.0.1:47830"
	// Scope is the only scope wow-mcp asks for.
	Scope = "wow.profile"

	// OpenPath is the macOS opener, always run by absolute path.
	OpenPath = "/usr/bin/open"

	userAgent        = "wow-mcp/0.1.0"
	maxBody          = 4 << 20
	maxErrorBody     = 200
	tokenRefreshSkew = 60 * time.Second
	defaultLoginWait = 5 * time.Minute
)

// ErrLoginExpired means there is no usable Battle.net user token.
var ErrLoginExpired = errors.New("Your Battle.net login has expired; run wow-mcp login.")

// KeyStore is the part of the Keychain the OAuth layer uses; keychain.Keychain
// satisfies it. Get returns keychain.ErrNotFound for a missing account.
type KeyStore interface {
	Get(ctx context.Context, account string) (string, error)
	Set(ctx context.Context, account, secret string) error
}

// Auth gets Battle.net tokens. The zero values of the optional fields are
// the real ones.
type Auth struct {
	TokenURL     string // default TokenURL
	AuthorizeURL string // default AuthorizeURL
	HTTP         *http.Client
	Keys         KeyStore
	Now          func() time.Time // default time.Now

	// Login hooks; tests replace them so no browser opens and no fixed port
	// is needed.
	Open         func(ctx context.Context, url string) error // default: /usr/bin/open
	Listen       func() (net.Listener, error)                // default: ListenAddr
	LoginTimeout time.Duration                               // default 5 minutes

	mu        sync.Mutex
	appToken  string
	appExpiry time.Time
}

// scrubbed is an error whose text has had secrets removed, while Unwrap still
// reaches the original, so errors.Is(err, context.Canceled) keeps working.
type scrubbed struct {
	msg string
	err error
}

func (e *scrubbed) Error() string { return e.msg }
func (e *scrubbed) Unwrap() error { return e.err }

// scrubErr prefixes err with prefix and removes every secret from its text.
func scrubErr(prefix string, err error, secrets ...string) error {
	return &scrubbed{msg: prefix + ": " + scrubText(err.Error(), secrets...), err: err}
}

func scrubText(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[secret]")
		}
	}
	return s
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Auth) tokenURL() string {
	if a.TokenURL != "" {
		return a.TokenURL
	}
	return TokenURL
}

func (a *Auth) authorizeURL() string {
	if a.AuthorizeURL != "" {
		return a.AuthorizeURL
	}
	return AuthorizeURL
}

// httpClient returns a copy of the configured client that follows no
// redirects: one could carry the Authorization header to another host.
func httpClient(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	cp := *c
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cp
}

// credentials reads the client id and secret from the Keychain.
func (a *Auth) credentials(ctx context.Context) (id, secret string, err error) {
	for _, acct := range []string{keychain.BlizzardClientID, keychain.BlizzardClientSecret} {
		v, err := a.Keys.Get(ctx, acct)
		if err != nil {
			return "", "", fmt.Errorf("Blizzard %s (store it with: wow-mcp keys set %s): %w", acct, acct, err)
		}
		if acct == keychain.BlizzardClientID {
			id = v
		} else {
			secret = v
		}
	}
	return id, secret, nil
}

// AppToken returns an application (client credentials) token, from memory
// until 60 seconds before it expires.
func (a *Auth) AppToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.appToken != "" && a.now().Add(tokenRefreshSkew).Before(a.appExpiry) {
		return a.appToken, nil
	}
	id, secret, err := a.credentials(ctx)
	if err != nil {
		return "", err
	}
	tok, expires, err := a.exchange(ctx, id, secret, url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return "", err
	}
	a.appToken, a.appExpiry = tok, expires
	return tok, nil
}

// forgetAppToken drops the cached application token (after a 401).
func (a *Auth) forgetAppToken() {
	a.mu.Lock()
	a.appToken, a.appExpiry = "", time.Time{}
	a.mu.Unlock()
}

// UserToken returns the stored Battle.net user token, or ErrLoginExpired when
// there is none or it has expired.
func (a *Auth) UserToken(ctx context.Context) (string, error) {
	exp, err := a.Keys.Get(ctx, keychain.BlizzardUserExpiry)
	if errors.Is(err, keychain.ErrNotFound) {
		return "", ErrLoginExpired
	}
	if err != nil {
		return "", fmt.Errorf("Battle.net login expiry: %w", err)
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(exp), 10, 64)
	if err != nil || !a.now().Before(time.Unix(secs, 0)) {
		return "", ErrLoginExpired
	}
	tok, err := a.Keys.Get(ctx, keychain.BlizzardUserToken)
	if errors.Is(err, keychain.ErrNotFound) {
		return "", ErrLoginExpired
	}
	if err != nil {
		return "", fmt.Errorf("Battle.net login token: %w", err)
	}
	return tok, nil
}

// exchange posts form to the token endpoint with HTTP Basic client auth and
// returns the access token and when it expires.
func (a *Auth) exchange(ctx context.Context, id, secret string, form url.Values) (string, time.Time, error) {
	hide := []string{secret, form.Get("code")}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, scrubErr("Battle.net token request", err, hide...)
	}
	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient(a.HTTP).Do(req)
	if err != nil {
		return "", time.Time{}, scrubErr("Battle.net token request", err, hide...)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", time.Time{}, &HTTPError{Status: resp.StatusCode, Body: readErrorBody(resp.Body, hide...)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return "", time.Time{}, scrubErr("Battle.net token answer", err, hide...)
	}
	if len(b) > maxBody {
		return "", time.Time{}, errors.New("Battle.net token answer too large")
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &tr); err != nil || tr.AccessToken == "" || tr.ExpiresIn <= 0 {
		return "", time.Time{}, errors.New("Battle.net token answer had no access_token or expires_in")
	}
	return tr.AccessToken, a.now().Add(time.Duration(tr.ExpiresIn) * time.Second), nil
}

// readErrorBody returns at most maxErrorBody bytes of an error answer with
// secrets removed.
func readErrorBody(r io.Reader, secrets ...string) string {
	longest := 0
	for _, s := range secrets {
		longest = max(longest, len(s))
	}
	b, _ := io.ReadAll(io.LimitReader(r, int64(maxErrorBody+longest)))
	msg := scrubText(string(b), secrets...)
	if len(msg) > maxErrorBody {
		msg = msg[:maxErrorBody]
	}
	return strings.TrimSpace(strings.ToValidUTF8(msg, ""))
}

// Login runs the browser authorization-code flow: it opens the authorize URL,
// waits for one callback on the fixed local port, checks state, exchanges the
// code, stores the user token and its expiry (Unix seconds) in the Keychain
// and returns when the token expires.
func (a *Auth) Login(ctx context.Context) (time.Time, error) {
	id, secret, err := a.credentials(ctx)
	if err != nil {
		return time.Time{}, err
	}
	var sb [16]byte
	if _, err := rand.Read(sb[:]); err != nil {
		return time.Time{}, fmt.Errorf("random state: %w", err)
	}
	state := hex.EncodeToString(sb[:])

	listen := a.Listen
	if listen == nil {
		listen = func() (net.Listener, error) { return net.Listen("tcp", ListenAddr) }
	}
	ln, err := listen()
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot listen for the Battle.net redirect (is another wow-mcp login running?): %w", err)
	}

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	send := func(r result) {
		select {
		case done <- r:
		default:
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var res result
		switch {
		case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1:
			res.err = errors.New("the Battle.net redirect carried the wrong state; login abandoned")
		case q.Get("error") != "":
			res.err = fmt.Errorf("Battle.net refused the login: %s", sanitizeParam(q.Get("error")))
			if d := q.Get("error_description"); d != "" {
				res.err = fmt.Errorf("%w (%s)", res.err, sanitizeParam(d))
			}
		case q.Get("code") == "":
			res.err = errors.New("the Battle.net redirect carried no code")
		default:
			res.code = q.Get("code")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if res.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "wow-mcp login failed. You can close this tab and read the terminal.")
		} else {
			fmt.Fprintln(w, "wow-mcp login is done. You can close this tab.")
		}
		send(res)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	wait := a.LoginTimeout
	if wait <= 0 {
		wait = defaultLoginWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	q := url.Values{
		"response_type": {"code"},
		"client_id":     {id},
		"redirect_uri":  {RedirectURI},
		"scope":         {Scope},
		"state":         {state},
	}
	authURL := a.authorizeURL() + "?" + q.Encode()
	open := a.Open
	if open == nil {
		open = openInBrowser
	}
	if err := open(ctx, authURL); err != nil {
		return time.Time{}, fmt.Errorf("opening the browser: %w", err)
	}

	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return time.Time{}, fmt.Errorf("no Battle.net redirect arrived within %s; login abandoned", wait)
		}
		return time.Time{}, ctx.Err()
	}
	if res.err != nil {
		return time.Time{}, res.err
	}
	// The callback is done; stop listening before the exchange.
	_ = srv.Close()

	tok, expires, err := a.exchange(ctx, id, secret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {res.code},
		"redirect_uri": {RedirectURI},
	})
	if err != nil {
		return time.Time{}, err
	}
	// Expiry first: with a stale token and a fresh expiry the worst case is a
	// 401, which maps to ErrLoginExpired; the other order could pair an old
	// expiry with the new token.
	if err := a.Keys.Set(ctx, keychain.BlizzardUserExpiry, strconv.FormatInt(expires.Unix(), 10)); err != nil {
		return time.Time{}, scrubErr("storing the login expiry", err, tok, secret)
	}
	if err := a.Keys.Set(ctx, keychain.BlizzardUserToken, tok); err != nil {
		return time.Time{}, scrubErr("storing the login token", err, tok, secret)
	}
	return expires, nil
}

// sanitizeParam keeps an error parameter from the browser short and printable.
func sanitizeParam(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > maxErrorBody {
		s = s[:maxErrorBody]
	}
	return strings.ToValidUTF8(s, "")
}

func openInBrowser(ctx context.Context, u string) error {
	return exec.CommandContext(ctx, OpenPath, u).Run()
}
