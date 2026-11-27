// Package zentao implements the authenticated, read-only Zentao HTTP client.
package zentao

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultReferer         = "/biz/"
	DefaultMaxResponseSize = 8 << 20
	defaultRecPerPage      = 100
)

var (
	ErrLoginFailed        = errors.New("zentao login failed")
	ErrSessionExpired     = errors.New("zentao session expired or login page returned")
	ErrResponseTooLarge   = errors.New("zentao response exceeds size limit")
	ErrInvalidBaseURL     = errors.New("invalid zentao base URL")
	ErrMissingCredentials = errors.New("zentao account and password are required")
	ErrInvalidPagination  = errors.New("zentao pageID and recPerPage must be positive")

	loginPathPattern = regexp.MustCompile(`(?i)user-login(?:-[a-z0-9+/]+={0,2})?\.html`)
)

// Client logs in and fetches raw assigned-item pages using a persistent cookie jar.
type Client struct {
	BaseURL          string
	Account          string
	Password         string
	HTTPClient       *http.Client
	Jar              http.CookieJar
	MaxResponseBytes int64
}

// New creates a Zentao client with an isolated cookie jar.
func New(baseURL, account, password string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidBaseURL
	}
	account = strings.TrimSpace(account)
	password = strings.TrimSpace(password)
	if account == "" || password == "" {
		return nil, ErrMissingCredentials
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create zentao cookie jar: %w", err)
	}
	return &Client{
		BaseURL:          strings.TrimRight(parsed.String(), "/") + "/",
		Account:          account,
		Password:         password,
		HTTPClient:       &http.Client{Jar: jar},
		Jar:              jar,
		MaxResponseBytes: DefaultMaxResponseSize,
	}, nil
}

// Login obtains a fresh challenge and submits Zentao's AJAX login form.
func (c *Client) Login(ctx context.Context) error {
	challenge, err := c.randomChallenge(ctx)
	if err != nil {
		return err
	}
	form := url.Values{
		"account":          {c.Account},
		"password":         {passwordHash(c.Password, challenge)},
		"passwordStrength": {"0"},
		"referer":          {defaultReferer},
		"verifyRand":       {challenge},
		"keepLogin":        {"0"},
		"captcha":          {""},
	}
	body, err := c.request(ctx, http.MethodPost, "user-login.html", form, "application/x-www-form-urlencoded")
	if err != nil {
		return fmt.Errorf("submit zentao login: %w", err)
	}
	var result struct {
		Result  string `json:"result"`
		Locate  string `json:"locate"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return errors.New("invalid zentao login response")
	}
	if result.Result != "success" {
		return ErrLoginFailed
	}
	// The session cookie is already persisted. Zentao's locate target is only a
	// browser navigation hint, so no additional request is needed here.
	return nil
}

// FetchAssignedTasks returns the raw first page of the current user's assigned tasks.
func (c *Client) FetchAssignedTasks(ctx context.Context) ([]byte, error) {
	return c.FetchAssignedTasksPage(ctx, 1, defaultRecPerPage)
}

// FetchAssignedTasksPage returns one raw page of the current user's assigned tasks.
func (c *Client) FetchAssignedTasksPage(ctx context.Context, pageID, recPerPage int) ([]byte, error) {
	return c.fetchAssignedPage(ctx, "my-task-type=assignedTo.html", pageID, recPerPage)
}

// FetchAssignedBugs returns the raw first page of the current user's assigned bugs.
func (c *Client) FetchAssignedBugs(ctx context.Context) ([]byte, error) {
	return c.FetchAssignedBugsPage(ctx, 1, defaultRecPerPage)
}

// FetchAssignedBugsPage returns one raw page of the current user's assigned bugs.
func (c *Client) FetchAssignedBugsPage(ctx context.Context, pageID, recPerPage int) ([]byte, error) {
	return c.fetchAssignedPage(ctx, "my-bug-type=assignedTo.html", pageID, recPerPage)
}

func (c *Client) fetchAssignedPage(ctx context.Context, path string, pageID, recPerPage int) ([]byte, error) {
	if pageID <= 0 || recPerPage <= 0 {
		return nil, ErrInvalidPagination
	}
	query := url.Values{"pageID": {strconv.Itoa(pageID)}, "recPerPage": {strconv.Itoa(recPerPage)}}
	body, err := c.request(ctx, http.MethodGet, path+"?"+query.Encode(), nil, "")
	if err != nil {
		return nil, fmt.Errorf("fetch zentao assigned page: %w", err)
	}
	if isLoginPage(body) {
		return nil, ErrSessionExpired
	}
	return body, nil
}

func isLoginPage(body []byte) bool {
	content := strings.ToLower(string(body))
	const loginPath = "user-login.html"
	hasAccountField := strings.Contains(content, `name="account"`) ||
		strings.Contains(content, `name='account'`) || strings.Contains(content, `name=account`)
	hasPasswordField := strings.Contains(content, `name="password"`) ||
		strings.Contains(content, `name='password'`) || strings.Contains(content, `name=password`)
	hasLoginFormID := strings.Contains(content, `id="loginform"`) ||
		strings.Contains(content, `id='loginform'`) || strings.Contains(content, `id=loginform`)
	if hasLoginFormID || (strings.Contains(content, "<form") && hasAccountField && hasPasswordField) ||
		(strings.Contains(content, "<form") && hasPasswordField && strings.Contains(content, loginPath)) {
		return true
	}

	redirectMarkers := []string{
		"self.location", "location.href", "location.replace", "location.assign", "window.location",
		"location=", "http-equiv=\"refresh\"", "http-equiv='refresh'",
	}
	for _, match := range loginPathPattern.FindAllStringIndex(content, -1) {
		start := match[0] - 160
		if start < 0 {
			start = 0
		}
		end := match[1] + 80
		if end > len(content) {
			end = len(content)
		}
		nearby := content[start:end]
		for _, marker := range redirectMarkers {
			if strings.Contains(nearby, marker) {
				return true
			}
		}
	}
	return false
}

func (c *Client) randomChallenge(ctx context.Context) (string, error) {
	body, err := c.request(ctx, http.MethodGet, "user-refreshRandom.html", nil, "")
	if err != nil {
		return "", fmt.Errorf("get zentao login challenge: %w", err)
	}
	challenge := strings.TrimSpace(string(body))
	if challenge == "" {
		return "", errors.New("invalid zentao login challenge")
	}
	for _, char := range challenge {
		if char < '0' || char > '9' {
			return "", errors.New("invalid zentao login challenge")
		}
	}
	return challenge, nil
}

func passwordHash(password, challenge string) string {
	inner := md5.Sum([]byte(password))
	outer := md5.Sum([]byte(hex.EncodeToString(inner[:]) + challenge))
	return hex.EncodeToString(outer[:])
}

func (c *Client) request(ctx context.Context, method, path string, form url.Values, contentType string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create zentao request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if method == http.MethodPost {
		req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	} else {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// Transport and redirect errors can embed request URLs. Keep them out of
		// surfaced errors because a URL may contain a session token.
		return nil, errors.New("zentao request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("zentao returned HTTP status %d", resp.StatusCode)
	}
	limit := c.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultMaxResponseSize
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read zentao response: %w", err)
	}
	if int64(len(response)) > limit {
		return nil, ErrResponseTooLarge
	}
	return response, nil
}
