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
	"strings"
	"time"
)

const (
	defaultReferer         = "/biz/"
	DefaultMaxResponseSize = 8 << 20
	defaultRecPerPage      = 100
	defaultRequestTimeout  = 30 * time.Second
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
	RequestTimeout   time.Duration
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
		RequestTimeout:   defaultRequestTimeout,
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
	body, err := c.request(ctx, http.MethodPost, "index.php?m=user&f=login", form, "application/x-www-form-urlencoded")
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
	if pageID <= 0 || recPerPage <= 0 {
		return nil, ErrInvalidPagination
	}
	return c.fetchAssignedPage(ctx, assignedPagePath("task", pageID, recPerPage))
}

// FetchAssignedBugs returns the raw first page of the current user's assigned bugs.
func (c *Client) FetchAssignedBugs(ctx context.Context) ([]byte, error) {
	return c.FetchAssignedBugsPage(ctx, 1, defaultRecPerPage)
}

// FetchAssignedBugsPage returns one raw page of the current user's assigned bugs.
func (c *Client) FetchAssignedBugsPage(ctx context.Context, pageID, recPerPage int) ([]byte, error) {
	if pageID <= 0 || recPerPage <= 0 {
		return nil, ErrInvalidPagination
	}
	return c.fetchAssignedPage(ctx, assignedPagePath("bug", pageID, recPerPage))
}

// FetchAssignedStories returns the raw first page of the current user's assigned stories.
func (c *Client) FetchAssignedStories(ctx context.Context) ([]byte, error) {
	return c.FetchAssignedStoriesPage(ctx, 1, defaultRecPerPage)
}

// FetchAssignedStoriesPage returns one raw page of the current user's assigned stories.
func (c *Client) FetchAssignedStoriesPage(ctx context.Context, pageID, recPerPage int) ([]byte, error) {
	if pageID <= 0 || recPerPage <= 0 {
		return nil, ErrInvalidPagination
	}
	return c.fetchAssignedPage(ctx, assignedPagePath("story", pageID, recPerPage))
}

// FetchAssignedPage 返回一页按指定排序的当前用户指派事项原始数据。
// objectType 仅允许任务/Bug/需求，orderBy 仅允许禅道支持的排序关键字，
// 避免拼接进查询串。
func (c *Client) FetchAssignedPage(ctx context.Context, objectType string, pageID, recPerPage int, orderBy string) ([]byte, error) {
	if pageID <= 0 || recPerPage <= 0 {
		return nil, ErrInvalidPagination
	}
	switch objectType {
	case "task", "bug", "story":
	default:
		return nil, ErrInvalidPagination
	}
	switch orderBy {
	case "id_desc", "lastEditedDate_desc", "openedDate_desc":
	default:
		return nil, ErrInvalidPagination
	}
	return c.fetchAssignedPage(ctx, assignedPagePathOrdered(objectType, orderBy, pageID, recPerPage))
}

func assignedPagePathOrdered(objectType, orderBy string, pageID, recPerPage int) string {
	return fmt.Sprintf("index.php?m=my&f=%s&browseType=assignedTo&param=0&orderBy=%s&recTotal=0&recPerPage=%d&pageID=%d", objectType, url.QueryEscape(orderBy), recPerPage, pageID)
}

func assignedPagePath(objectType string, pageID, recPerPage int) string {
	return fmt.Sprintf("index.php?m=my&f=%s&browseType=assignedTo&param=0&orderBy=id_desc&recTotal=0&recPerPage=%d&pageID=%d", objectType, recPerPage, pageID)
}

func (c *Client) fetchAssignedPage(ctx context.Context, path string) ([]byte, error) {
	body, err := c.request(ctx, http.MethodGet, path, nil, "")
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
	body, err := c.request(ctx, http.MethodGet, "index.php?m=user&f=refreshRandom", nil, "")
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
	timeout := c.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
		req.Header.Set("Referer", c.BaseURL+"index.php?m=user&f=login")
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
