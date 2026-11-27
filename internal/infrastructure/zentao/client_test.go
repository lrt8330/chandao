package zentao

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		account  string
		password string
	}{
		{name: "malformed URL", baseURL: "://bad", account: "alice", password: "test-password"},
		{name: "relative URL", baseURL: "/biz/", account: "alice", password: "test-password"},
		{name: "unsupported scheme", baseURL: "ftp://example.com/biz/", account: "alice", password: "test-password"},
		{name: "URL credentials", baseURL: "http://alice:test-password@example.com/biz/", account: "alice", password: "test-password"},
		{name: "URL query", baseURL: "http://example.com/biz/?token=secret-token", account: "alice", password: "test-password"},
		{name: "URL fragment", baseURL: "http://example.com/biz/#secret-token", account: "alice", password: "test-password"},
		{name: "empty account", baseURL: "http://example.com/biz/", account: " ", password: "test-password"},
		{name: "empty password", baseURL: "http://example.com/biz/", account: "alice", password: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.baseURL, tt.account, tt.password)
			if client != nil {
				t.Errorf("New client = %#v, want nil", client)
			}
			assertErrorOmits(t, err, "test-password", "secret-token")
		})
	}

	client, err := New(" http://example.com/biz ", " alice ", " test-password ")
	if err != nil {
		t.Fatalf("New valid configuration: %v", err)
	}
	if client.BaseURL != "http://example.com/biz/" || client.Account != "alice" || client.Password != "test-password" {
		t.Errorf("normalized client configuration = %#v", client)
	}
	if client.HTTPClient == nil || client.Jar == nil || client.HTTPClient.Jar != client.Jar {
		t.Error("New did not attach an isolated cookie jar to its HTTP client")
	}
	if client.MaxResponseBytes != DefaultMaxResponseSize {
		t.Errorf("MaxResponseBytes = %d, want %d", client.MaxResponseBytes, DefaultMaxResponseSize)
	}
	if client.RequestTimeout != defaultRequestTimeout {
		t.Errorf("RequestTimeout = %v, want %v", client.RequestTimeout, defaultRequestTimeout)
	}
}

func TestLoginSubmitsVerifiedProtocol(t *testing.T) {
	const password = "test-password"
	const challenge = "123456"
	wantPassword := md5Hex(md5Hex(password) + challenge)

	var challengeRequests atomic.Int32
	var loginRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case isRefreshRandomRequest(r):
			challengeRequests.Add(1)
			if r.Method != http.MethodGet {
				t.Errorf("challenge method = %q, want GET", r.Method)
			}
			fmt.Fprint(w, " \n"+challenge+"\n ")
		case isLoginRequest(r):
			loginRequests.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("login method = %q, want POST", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Errorf("Content-Type = %q, want form encoding", got)
			}
			if got, want := r.Header.Get("Referer"), "http://"+r.Host+"/biz/index.php?m=user&f=login"; got != want {
				t.Errorf("Referer = %q, want %q", got, want)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			wantForm := map[string]string{
				"account":          "alice",
				"password":         wantPassword,
				"passwordStrength": "0",
				"referer":          "/biz/",
				"verifyRand":       challenge,
				"keepLogin":        "0",
				"captcha":          "",
			}
			for key, want := range wantForm {
				if got := r.PostForm.Get(key); got != want {
					t.Errorf("form[%q] = %q, want %q", key, got, want)
				}
			}
			if _, ok := r.PostForm["captcha"]; !ok {
				t.Error("captcha field is absent, want an explicitly empty field")
			}
			http.SetCookie(w, &http.Cookie{Name: "zentaosid", Value: "test-session", Path: "/biz/", HttpOnly: true})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"result":"success","locate":"/biz/"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", " alice ", " "+password+" ")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := challengeRequests.Load(); got != 1 {
		t.Errorf("challenge requests = %d, want 1", got)
	}
	if got := loginRequests.Load(); got != 1 {
		t.Errorf("login requests = %d, want 1", got)
	}
}

func TestFetchAssignedURLsAndCookiePersistence(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		response string
		fetch    func(context.Context, *Client) ([]byte, error)
	}{
		{
			name:     "assigned tasks",
			method:   "task",
			response: "<html><body>assigned tasks</body></html>",
			fetch: func(ctx context.Context, client *Client) ([]byte, error) {
				return client.FetchAssignedTasks(ctx)
			},
		},
		{
			name:     "assigned bugs",
			method:   "bug",
			response: "<html><body>assigned bugs</body></html>",
			fetch: func(ctx context.Context, client *Client) ([]byte, error) {
				return client.FetchAssignedBugs(ctx)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pageRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case isRefreshRandomRequest(r):
					fmt.Fprint(w, "654321")
				case isLoginRequest(r):
					http.SetCookie(w, &http.Cookie{Name: "zentaosid", Value: "persisted-test-session", Path: "/biz/", HttpOnly: true})
					writeJSON(t, w, map[string]string{"result": "success", "locate": "/biz/"})
				case isAssignedRequest(r, tt.method):
					pageRequests.Add(1)
					if r.Method != http.MethodGet {
						t.Errorf("page method = %q, want GET", r.Method)
					}
					if got := r.URL.Query().Get("pageID"); got != "1" {
						t.Errorf("pageID = %q, want 1", got)
					}
					if got := r.URL.Query().Get("recPerPage"); got != "100" {
						t.Errorf("recPerPage = %q, want 100", got)
					}
					cookie, err := r.Cookie("zentaosid")
					if err != nil || cookie.Value != "persisted-test-session" {
						t.Errorf("session cookie = %v, %v; want persisted-test-session", cookie, err)
					}
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					fmt.Fprint(w, tt.response)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := client.Login(context.Background()); err != nil {
				t.Fatalf("Login: %v", err)
			}
			body, err := tt.fetch(context.Background(), client)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if got, want := string(body), tt.response; got != want {
				t.Errorf("body = %q, want %q", got, want)
			}
			if got := pageRequests.Load(); got != 1 {
				t.Errorf("page requests = %d, want 1", got)
			}
		})
	}
}

func TestFetchAssignedPagesUsePaginationQuery(t *testing.T) {
	tests := []struct {
		name   string
		method string
		fetch  func(context.Context, *Client, int, int) ([]byte, error)
	}{
		{
			name:   "tasks",
			method: "task",
			fetch: func(ctx context.Context, client *Client, pageID, recPerPage int) ([]byte, error) {
				return client.FetchAssignedTasksPage(ctx, pageID, recPerPage)
			},
		},
		{
			name:   "bugs",
			method: "bug",
			fetch: func(ctx context.Context, client *Client, pageID, recPerPage int) ([]byte, error) {
				return client.FetchAssignedBugsPage(ctx, pageID, recPerPage)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !isAssignedRequest(r, tt.method) {
					t.Errorf("unexpected assigned page request: %s", r.URL.RequestURI())
				}
				requests.Add(1)
				if got := r.URL.Query().Get("pageID"); got != "3" {
					t.Errorf("pageID = %q, want 3", got)
				}
				if got := r.URL.Query().Get("recPerPage"); got != "25" {
					t.Errorf("recPerPage = %q, want 25", got)
				}
				fmt.Fprint(w, "page content")
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			body, err := tt.fetch(context.Background(), client, 3, 25)
			if err != nil {
				t.Fatalf("fetch page: %v", err)
			}
			if string(body) != "page content" || requests.Load() != 1 {
				t.Errorf("body = %q, requests = %d", body, requests.Load())
			}
		})
	}
}

func TestFetchAssignedPagesRejectNonPositivePagination(t *testing.T) {
	client, err := New("http://example.com/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, values := range [][2]int{{0, 100}, {-1, 100}, {1, 0}, {1, -1}} {
		if body, err := client.FetchAssignedTasksPage(context.Background(), values[0], values[1]); body != nil || !errors.Is(err, ErrInvalidPagination) {
			t.Errorf("tasks page (%d, %d) = %q, %v; want nil, ErrInvalidPagination", values[0], values[1], body, err)
		}
		if body, err := client.FetchAssignedBugsPage(context.Background(), values[0], values[1]); body != nil || !errors.Is(err, ErrInvalidPagination) {
			t.Errorf("bugs page (%d, %d) = %q, %v; want nil, ErrInvalidPagination", values[0], values[1], body, err)
		}
	}
}

func TestAssignedPageRejectsLoginContent(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "login.html"))
	if err != nil {
		t.Fatalf("read login fixture: %v", err)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "sanitized login form", body: string(fixture)},
		{name: "login form without id or login action", body: `<html><form method="post" action="/biz/"><input type="text" name="account"><input type="password" name="password"></form></html>`},
		{name: "pretty login redirect", body: "<html><meta charset='utf-8'/><style>body{background:white}</style><script>self.location='/biz/user-login-L2Jpei8=.html';\n\n</script></html>"},
		{name: "location href redirect", body: `<html><script>window.location.href = "/biz/user-login.html";</script></html>`},
		{name: "location replace redirect", body: `<html><script>window.location.replace('/biz/user-login.html?referer=%2Fbiz%2F');</script></html>`},
		{name: "meta refresh redirect", body: `<html><head><meta http-equiv="refresh" content="0; url=/biz/user-login.html"></head></html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !isAssignedRequest(r, "task") {
					t.Errorf("unexpected assigned page request: %s", r.URL.RequestURI())
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			body, err := client.FetchAssignedTasks(context.Background())
			if body != nil {
				t.Errorf("body = %q, want nil for a login page", body)
			}
			if !errors.Is(err, ErrSessionExpired) {
				t.Fatalf("error = %v, want ErrSessionExpired", err)
			}
		})
	}
}

func isAssignedRequest(r *http.Request, method string) bool {
	return r.URL.Path == "/biz/index.php" &&
		r.URL.Query().Get("m") == "my" &&
		r.URL.Query().Get("f") == method &&
		r.URL.Query().Get("browseType") == "assignedTo"
}

func isLoginRequest(r *http.Request) bool {
	return r.URL.Path == "/biz/index.php" &&
		r.URL.Query().Get("m") == "user" &&
		r.URL.Query().Get("f") == "login"
}

func isRefreshRandomRequest(r *http.Request) bool {
	return r.URL.Path == "/biz/index.php" &&
		r.URL.Query().Get("m") == "user" &&
		r.URL.Query().Get("f") == "refreshRandom"
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestLoginRandomChallengeValidation(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		maxResponse  int64
		wantTooLarge bool
	}{
		{name: "empty challenge", status: http.StatusOK, body: " \n\t "},
		{name: "non numeric challenge", status: http.StatusOK, body: "12ab56"},
		{name: "json is not a plain challenge", status: http.StatusOK, body: `{"verifyRand":123}`},
		{name: "non 2xx challenge", status: http.StatusServiceUnavailable, body: "sensitive challenge response"},
		{name: "oversized challenge", status: http.StatusOK, body: strings.Repeat("1", 33), maxResponse: 32, wantTooLarge: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var loginRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if isLoginRequest(r) {
					loginRequests.Add(1)
					writeJSON(t, w, map[string]string{"result": "success"})
					return
				}
				if !isRefreshRandomRequest(r) {
					t.Errorf("unexpected request %s", r.URL.RequestURI())
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if tt.maxResponse > 0 {
				client.MaxResponseBytes = tt.maxResponse
			}
			err = client.Login(context.Background())
			if tt.wantTooLarge {
				if !errors.Is(err, ErrResponseTooLarge) {
					t.Fatalf("error = %v, want ErrResponseTooLarge", err)
				}
			} else if err == nil {
				t.Fatal("Login succeeded with an invalid challenge")
			}
			assertErrorOmits(t, err, "test-password", "sensitive challenge response")
			if got := loginRequests.Load(); got != 0 {
				t.Errorf("login requests = %d, want 0", got)
			}
		})
	}
}

func TestLoginPasswordHashCases(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{name: "simple password", password: "simple-password"},
		{name: "surrounding whitespace is trimmed", password: "  padded-password\t"},
		{name: "utf8 and form punctuation", password: " päss&+word "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const challenge = "000042"
			trimmed := strings.TrimSpace(tt.password)
			wantHash := md5Hex(md5Hex(trimmed) + challenge)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case isRefreshRandomRequest(r):
					fmt.Fprint(w, challenge)
				case isLoginRequest(r):
					if err := r.ParseForm(); err != nil {
						t.Errorf("ParseForm: %v", err)
					}
					if got := r.PostForm.Get("password"); got != wantHash {
						t.Errorf("password hash = %q, want %q", got, wantHash)
					}
					if got := r.PostForm.Get("account"); got != "test-user" {
						t.Errorf("account = %q, want test-user", got)
					}
					writeJSON(t, w, map[string]string{"result": "success", "locate": "/biz/"})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", " test-user ", tt.password)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := client.Login(context.Background()); err != nil {
				t.Fatalf("Login: %v", err)
			}
		})
	}
}

func TestLoginJSONResultHandling(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		body            string
		wantSuccess     bool
		wantLoginFailed bool
	}{
		{name: "success without locate", status: http.StatusOK, body: `{"result":"success"}`, wantSuccess: true},
		{name: "success locate is not followed", status: http.StatusOK, body: `{"result":"success","locate":"/biz/"}`, wantSuccess: true},
		{name: "rejected credentials", status: http.StatusOK, body: `{"result":"fail","message":"test-password secret-token full-response-marker"}`, wantLoginFailed: true},
		{name: "missing result", status: http.StatusOK, body: `{"message":"full-response-marker"}`},
		{name: "malformed json", status: http.StatusOK, body: `{"result":`},
		{name: "html error page", status: http.StatusOK, body: `<html>full-response-marker</html>`},
		{name: "non 2xx login", status: http.StatusUnauthorized, body: "full-response-marker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var unexpectedRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case isRefreshRandomRequest(r):
					fmt.Fprint(w, "123456")
				case isLoginRequest(r):
					http.SetCookie(w, &http.Cookie{Name: "zentaosid", Value: "secret-cookie", Path: "/biz/", HttpOnly: true})
					w.WriteHeader(tt.status)
					fmt.Fprint(w, tt.body)
				case r.URL.Path == "/biz/":
					unexpectedRequests.Add(1)
					fmt.Fprint(w, "dashboard")
				default:
					unexpectedRequests.Add(1)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = client.Login(context.Background())
			if tt.wantSuccess {
				if err != nil {
					t.Fatalf("Login: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("Login succeeded, want an error")
				}
				if tt.wantLoginFailed && !errors.Is(err, ErrLoginFailed) {
					t.Errorf("error = %v, want ErrLoginFailed", err)
				}
				assertErrorOmits(t, err, "test-password", "secret-cookie", "secret-token", "full-response-marker")
			}
			if got := unexpectedRequests.Load(); got != 0 {
				t.Errorf("unexpected follow-up requests = %d, want 0", got)
			}
		})
	}
}

func TestAssignedPageAllowsLoginLinkInData(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "standard login link", body: `<html><body><a href="/biz/user-login.html">login documentation</a><p>assigned task data</p></body></html>`},
		{name: "pretty login link", body: `<html><body><a href="/biz/user-login-L2Jpei8=.html">login documentation</a><p>assigned task data</p></body></html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got, err := client.FetchAssignedBugs(context.Background())
			if err != nil {
				t.Fatalf("FetchAssignedBugs: %v", err)
			}
			if string(got) != tt.body {
				t.Errorf("body = %q, want assigned data", got)
			}
		})
	}
}

func TestFetchAssignedHTTPFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		fetch  func(context.Context, *Client) ([]byte, error)
	}{
		{name: "tasks unauthorized", status: http.StatusUnauthorized, fetch: func(ctx context.Context, client *Client) ([]byte, error) { return client.FetchAssignedTasks(ctx) }},
		{name: "tasks server error", status: http.StatusInternalServerError, fetch: func(ctx context.Context, client *Client) ([]byte, error) { return client.FetchAssignedTasks(ctx) }},
		{name: "bugs forbidden", status: http.StatusForbidden, fetch: func(ctx context.Context, client *Client) ([]byte, error) { return client.FetchAssignedBugs(ctx) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, "full-response-marker secret-cookie")
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			body, err := tt.fetch(context.Background(), client)
			if body != nil {
				t.Errorf("body = %q, want nil", body)
			}
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP status %d", tt.status)) {
				t.Fatalf("error = %v, want HTTP status %d", err, tt.status)
			}
			assertErrorOmits(t, err, "test-password", "full-response-marker", "secret-cookie")
		})
	}
}

func TestFetchAssignedRejectsOversizedBody(t *testing.T) {
	tests := []struct {
		name  string
		fetch func(context.Context, *Client) ([]byte, error)
	}{
		{name: "tasks", fetch: func(ctx context.Context, client *Client) ([]byte, error) { return client.FetchAssignedTasks(ctx) }},
		{name: "bugs", fetch: func(ctx context.Context, client *Client) ([]byte, error) { return client.FetchAssignedBugs(ctx) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, strings.Repeat("x", 65))
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			client.MaxResponseBytes = 64
			body, err := tt.fetch(context.Background(), client)
			if body != nil {
				t.Errorf("body length = %d, want nil", len(body))
			}
			if !errors.Is(err, ErrResponseTooLarge) {
				t.Fatalf("error = %v, want ErrResponseTooLarge", err)
			}
		})
	}
}

func TestClientHonorsCanceledContext(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, *Client) error
	}{
		{name: "login", call: func(ctx context.Context, client *Client) error { return client.Login(ctx) }},
		{name: "assigned tasks", call: func(ctx context.Context, client *Client) error {
			_, err := client.FetchAssignedTasks(ctx)
			return err
		}},
		{name: "assigned bugs", call: func(ctx context.Context, client *Client) error {
			_, err := client.FetchAssignedBugs(ctx)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}))
			defer server.Close()

			client, err := New(server.URL+"/biz/", "test-user", "test-password")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err = tt.call(ctx, client)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if got := requests.Load(); got != 0 {
				t.Errorf("requests = %d, want 0", got)
			}
		})
	}
}

func TestClientCancellationDuringRequest(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := client.FetchAssignedTasks(ctx)
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not stop after context cancellation")
	}
}

func TestClientRequestTimeout(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.RequestTimeout = 25 * time.Millisecond
	result := make(chan error, 1)
	go func() {
		_, err := client.FetchAssignedTasks(context.Background())
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not stop after its timeout")
	}
}

func TestRequestErrorOmitsSensitiveRedirectURL(t *testing.T) {
	const token = "secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isRefreshRandomRequest(r) {
			http.Redirect(w, r, "/failed?token="+token, http.StatusFound)
			return
		}
		panic(http.ErrAbortHandler)
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = client.Login(context.Background())
	assertErrorOmits(t, err, "test-password", token)
}

func assertErrorOmits(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Errorf("error contains sensitive value %q: %v", secret, err)
		}
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("write response: %v", err)
	}
}
