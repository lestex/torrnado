package flaresolverr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// solvedBody is the shape a real FlareSolverr 3.5.2 returns, recorded
// from one. The fields we ignore are kept so a decoder that choked on
// them would be caught here rather than against a live server.
const solvedBody = `{
  "status": "ok",
  "message": "Challenge solved!",
  "startTimestamp": 1792083709729,
  "endTimestamp": 1792083711026,
  "version": "3.5.2",
  "solution": {
    "url": "https://example.test/forum/index.php",
    "status": 200,
    "headers": {"content-type": "text/html"},
    "response": "<html><title>the real page</title></html>",
    "cookies": [
      {"domain": ".example.test", "expiry": 1823619711, "httpOnly": true, "name": "cf_clearance",
       "path": "/", "sameSite": "None", "secure": true, "value": "cleared"},
      {"domain": ".example.test", "httpOnly": false, "name": "bb_session", "path": "/",
       "sameSite": "Lax", "secure": false, "value": "sess-123"}
    ],
    "turnstile_token": "",
    "userAgent": "Mozilla/5.0 (X11; Linux x86_64) Chrome/152.0.0.0"
  }
}`

// serveFlare starts a stand-in FlareSolverr that records the command it
// was given and answers with body.
func serveFlare(t *testing.T, body string, status int) (*Client, *command) {
	t.Helper()

	var got command
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" {
			t.Errorf("posted to %q, want /v1", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Config{URL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, &got
}

// The HTML FlareSolverr already fetched is the whole point - taking its
// cookies and re-fetching ourselves is what cannot work.
func TestGetReturnsTheSolvedPage(t *testing.T) {
	c, sent := serveFlare(t, solvedBody, http.StatusOK)

	page, err := c.Get(context.Background(), "https://example.test/forum/index.php", Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !strings.Contains(page.HTML, "the real page") {
		t.Errorf("HTML = %q, want the solved page", page.HTML)
	}
	if page.Status != 200 {
		t.Errorf("Status = %d, want the page's own 200", page.Status)
	}
	if page.URL != "https://example.test/forum/index.php" {
		t.Errorf("URL = %q", page.URL)
	}
	if !strings.Contains(page.UserAgent, "Chrome") {
		t.Errorf("UserAgent = %q", page.UserAgent)
	}

	if v, ok := page.Cookie("bb_session"); !ok || v != "sess-123" {
		t.Errorf("bb_session = %q, %v", v, ok)
	}
	if _, ok := page.Cookie("nope"); ok {
		t.Error("Cookie reported one that is not there")
	}

	if sent.Cmd != "request.get" {
		t.Errorf("cmd = %q, want request.get", sent.Cmd)
	}
	if sent.MaxTimeout != DefaultTimeout.Milliseconds() {
		t.Errorf("maxTimeout = %d, want %d ms", sent.MaxTimeout, DefaultTimeout.Milliseconds())
	}
}

// Injecting a cookie is how a site login is carried in - the half of the
// problem that does transfer between clients.
func TestCookiesAndSessionAreSent(t *testing.T) {
	c, sent := serveFlare(t, solvedBody, http.StatusOK)

	_, err := c.Get(context.Background(), "https://example.test/", Options{
		Session: "sess-id",
		Cookies: []Cookie{{Name: "bb_session", Value: "abc", Domain: ".example.test"}},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if sent.Session != "sess-id" {
		t.Errorf("session = %q", sent.Session)
	}
	if len(sent.Cookies) != 1 || sent.Cookies[0].Name != "bb_session" || sent.Cookies[0].Value != "abc" {
		t.Errorf("cookies = %+v", sent.Cookies)
	}
}

// Verified against a live FlareSolverr: postData is the form already
// encoded, and it arrives at the site as
// application/x-www-form-urlencoded.
func TestPostSendsAnEncodedForm(t *testing.T) {
	c, sent := serveFlare(t, solvedBody, http.StatusOK)

	form := url.Values{"login_username": {"bob"}, "login_password": {"hunter 2"}, "login": {"Login"}}
	if _, err := c.Post(context.Background(), "https://example.test/forum/login.php", form, Options{}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if sent.Cmd != "request.post" {
		t.Errorf("cmd = %q, want request.post", sent.Cmd)
	}
	parsed, err := url.ParseQuery(sent.PostData)
	if err != nil {
		t.Fatalf("postData is not a form: %q", sent.PostData)
	}
	if parsed.Get("login_username") != "bob" || parsed.Get("login_password") != "hunter 2" {
		t.Errorf("postData = %q, want the form round-tripped with its spaces intact", sent.PostData)
	}
}

// A cookie with no domain is sent without one, which is what makes the
// browser bind it to the page being fetched. Sending an empty string
// instead would be a domain that matches nothing, and a mismatch fails
// the whole request rather than just dropping the cookie.
func TestACookieWithNoDomainSendsNone(t *testing.T) {
	c, sent := serveFlare(t, solvedBody, http.StatusOK)

	_, err := c.Get(context.Background(), "https://example.test/", Options{
		Cookies: []Cookie{{Name: "bb_session", Value: "abc"}},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := sent.Cookies[0].Domain; got != "" {
		t.Errorf("domain = %q, want it absent", got)
	}

	// Belt and braces: the encoded body must not carry a "domain" key
	// at all, which omitempty is what guarantees.
	body, err := json.Marshal(command{Cookies: []Cookie{{Name: "n", Value: "v"}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "domain") {
		t.Errorf("encoded as %s, want no domain key", body)
	}
}

func TestSessionLifecycle(t *testing.T) {
	const created = `{"status":"ok","message":"Session created successfully.","session":"abc-123"}`
	c, sent := serveFlare(t, created, http.StatusOK)

	id, err := c.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("session = %q", id)
	}
	if sent.Cmd != "sessions.create" {
		t.Errorf("cmd = %q", sent.Cmd)
	}

	destroyed := `{"status":"ok","message":"The session has been removed."}`
	c2, sent2 := serveFlare(t, destroyed, http.StatusOK)
	if err := c2.DestroySession(context.Background(), "abc-123"); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}
	if sent2.Cmd != "sessions.destroy" || sent2.Session != "abc-123" {
		t.Errorf("cmd = %q session = %q", sent2.Cmd, sent2.Session)
	}
}

// FlareSolverr reports its own failures in the body with HTTP 500, so
// the status alone says nothing useful - its message is what a user can
// act on, and it has to survive.
func TestAFailureKeepsFlareSolverrsOwnWords(t *testing.T) {
	const failed = `{"status":"error","message":"Error solving the challenge. Timeout after 60.0 seconds."}`
	c, _ := serveFlare(t, failed, http.StatusInternalServerError)

	_, err := c.Get(context.Background(), "https://example.test/", Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Timeout after 60.0 seconds") {
		t.Errorf("error lost the reason: %v", err)
	}

	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatalf("error is %T, want *flaresolverr.Error", err)
	}
	if fe.Kind != KindServer {
		t.Errorf("kind = %v, want KindServer - a reply that arrived is not unreachable", fe.Kind)
	}
}

// "FlareSolverr is not running" and "it could not fetch that page" need
// different fixes, so they have to be distinguishable.
func TestAnUnreachableServerSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()

	c, err := New(Config{URL: addr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.Get(context.Background(), "https://example.test/", Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindUnreachable {
		t.Fatalf("want an unreachable *Error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "is it running") {
		t.Errorf("message should suggest the obvious cause: %v", err)
	}
}

// A solution-less "ok" would otherwise be a nil dereference.
func TestAnOkReplyWithNoPage(t *testing.T) {
	c, _ := serveFlare(t, `{"status":"ok","message":"nothing to say"}`, http.StatusOK)

	if _, err := c.Get(context.Background(), "https://example.test/", Options{}); err == nil {
		t.Fatal("expected an error when no page came back")
	}
}

func TestNewValidatesTheURL(t *testing.T) {
	for _, bad := range []string{"", "   ", "localhost:8191", "ftp://x", "http://"} {
		if _, err := New(Config{URL: bad}); err == nil {
			t.Errorf("New(%q) should have failed", bad)
		}
	}
	// A trailing slash must not become "//v1".
	c, err := New(Config{URL: "http://localhost:8191/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.URL() != "http://localhost:8191" {
		t.Errorf("URL() = %q, want the slash trimmed", c.URL())
	}
}

func TestTimeoutDefaults(t *testing.T) {
	c, err := New(Config{URL: "http://localhost:8191"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want %s", c.timeout, DefaultTimeout)
	}
	// Our own deadline has to outlast the one we give FlareSolverr, or
	// we hang up on a server that was about to answer.
	if c.http.Timeout <= c.timeout {
		t.Errorf("http timeout %s should exceed maxTimeout %s", c.http.Timeout, c.timeout)
	}

	c2, _ := New(Config{URL: "http://localhost:8191", Timeout: 5 * time.Second})
	if c2.timeout != 5*time.Second {
		t.Errorf("timeout = %s, want it honoured", c2.timeout)
	}
}

// serveHanging starts a server that never answers until the test lets
// it go.
//
// The handler waits on a channel the test closes rather than on the
// request's own context: if it waited on the context and that never
// fired, httptest's Close would block forever waiting for the handler,
// and a test that fails by hanging is a bad test.
func serveHanging(t *testing.T, timeout time.Duration, httpClient *http.Client) *Client {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	c, err := New(Config{URL: srv.URL, Timeout: timeout, slack: 30 * time.Millisecond, HTTP: httpClient})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// A server that is there and slow is not a server that is absent, and
// the fixes differ: more time against a healthy server, versus starting
// one or correcting the address. Telling someone watching it run that it
// may not be running is how an afternoon gets lost.
func TestASlowServerIsATimeoutNotUnreachable(t *testing.T) {
	c := serveHanging(t, 20*time.Millisecond, nil)

	_, err := c.Get(context.Background(), "https://example.test/", Options{})
	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatalf("error is %T, want *Error: %v", err, err)
	}
	if fe.Kind != KindTimeout {
		t.Errorf("kind = %v, want KindTimeout", fe.Kind)
	}
	if strings.Contains(err.Error(), "is it running") {
		t.Errorf("a timeout must not advise restarting a healthy server: %v", err)
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("message should point at the timeout: %v", err)
	}
}

// A caller giving up - a daemon shutting down mid-search - is not a
// failure of anything, so nothing is diagnosed.
func TestACancelledRequestSaysSo(t *testing.T) {
	c := serveHanging(t, 5*time.Second, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := c.Get(ctx, "https://example.test/", Options{})
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindCanceled {
		t.Fatalf("want KindCanceled, got %v: %v", fe, err)
	}
	if strings.Contains(err.Error(), "is it running") {
		t.Errorf("cancelling is nobody's fault: %v", err)
	}
}

// A failure that will not say why still has its status, which is then
// the only fact left worth reporting.
func TestAFailureWithNoMessageStillReportsTheStatus(t *testing.T) {
	c, _ := serveFlare(t, `{"status":"error"}`, http.StatusBadGateway)

	_, err := c.Get(context.Background(), "https://example.test/", Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Errorf("message rendered a nil error: %v", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("message should carry the status, the only fact left: %v", err)
	}
}

// Something that answers but is not FlareSolverr at all - a reverse
// proxy's error page, or another service on that port.
func TestANonFlareSolverrReply(t *testing.T) {
	c, _ := serveFlare(t, "<html>nginx error page</html>", http.StatusBadGateway)

	_, err := c.Get(context.Background(), "https://example.test/", Options{})
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindBadReply {
		t.Fatalf("want KindBadReply, got %v: %v", fe, err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("message should carry the status: %v", err)
	}
}

// The deadline has to hold whichever http.Client is in use. A caller
// supplying its own - to share a transport - would otherwise wait
// forever on a server that accepts the connection and never answers.
func TestACallerSuppliedClientIsStillBounded(t *testing.T) {
	// A client with no Timeout of its own, as a caller sharing a
	// transport would supply.
	c := serveHanging(t, 20*time.Millisecond, &http.Client{})

	done := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background(), "https://example.test/", Options{})
		done <- err
	}()

	select {
	case err := <-done:
		var fe *Error
		if !errors.As(err, &fe) || fe.Kind != KindTimeout {
			t.Errorf("want KindTimeout, got %v: %v", fe, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a caller-supplied client with no Timeout was never bounded")
	}
}
