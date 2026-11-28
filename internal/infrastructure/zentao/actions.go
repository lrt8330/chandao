// 禅道事项操作：抓取操作页表单（含隐藏 token 与页面预填值），原样回填
// 后提交——等效于在禅道页面上打开操作弹窗直接点确定。
package zentao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

var (
	// ErrActionFormNotFound 操作页里找不到可提交的表单。
	ErrActionFormNotFound = errors.New("zentao action form not found")
	// ErrActionFormTarget 表单目标不在当前禅道实例内。
	ErrActionFormTarget = errors.New("zentao action form target is outside the instance")
	// ErrActionRejected 禅道明确拒绝本次操作（校验错误或业务失败）。
	ErrActionRejected = errors.New("zentao rejected the action")
	// ErrActionOutcomeUnknown 已提交但无法确认结果；界面应提示刷新核实，
	// 而不是自动重试，避免重复执行写操作。
	ErrActionOutcomeUnknown = errors.New("zentao action outcome unknown")
)

// ActionForm 是从禅道操作页解析出的表单：目标路径与全部字段。多值字段
// （多选、多个勾选的复选框）保留全部值，与浏览器提交一致。
type ActionForm struct {
	// Path 是表单的提交地址（相对 BaseURL）。
	Path string
	// Fields 含隐藏域（token 等）、输入框/下拉/文本域的页面预填值。
	Fields url.Values
}

// FetchActionForm 抓取 objectType 的 action 操作页并解析表单。
// 页面地址形如 index.php?m=task&f=start&taskID=123。
func (c *Client) FetchActionForm(ctx context.Context, objectType, action, id string) (*ActionForm, error) {
	path, err := actionPagePath(objectType, action, id)
	if err != nil {
		return nil, err
	}
	body, err := c.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, fmt.Errorf("fetch zentao %s %s page: %w", objectType, action, err)
	}
	if isLoginPage(body) {
		return nil, ErrSessionExpired
	}
	form, err := parseActionForm(body, objectType, c.BaseURL+path, c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse zentao %s %s form: %w", objectType, action, err)
	}
	return form, nil
}

// FetchActionPage 返回操作页原始 HTML（zentaoctl 存夹具用）。
func (c *Client) FetchActionPage(ctx context.Context, objectType, action, id string) ([]byte, error) {
	path, err := actionPagePath(objectType, action, id)
	if err != nil {
		return nil, err
	}
	body, err := c.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, fmt.Errorf("fetch zentao %s %s page: %w", objectType, action, err)
	}
	if isLoginPage(body) {
		return nil, ErrSessionExpired
	}
	return body, nil
}

// actionPagePath 校验对象与动作白名单后拼出操作页地址。
func actionPagePath(objectType, action, id string) (string, error) {
	switch objectType {
	case "task", "bug", "story":
	default:
		return "", ErrInvalidPagination
	}
	switch action {
	case "start", "finish", "pause", "cancel", "close", "activate", "resolve", "confirm", "assign", "transfer", "recordEstimate":
	default:
		return "", ErrInvalidPagination
	}
	return fmt.Sprintf("index.php?m=%s&f=%s&%sID=%s", objectType, action, objectType, url.QueryEscape(id)), nil
}

// SubmitAction 提交操作表单。overrides 里的字段覆盖页面预填值（如备注）。
// 只有响应明确表示成功才返回 nil；登录页说明会话失效；业务失败返回
// ErrActionRejected；其余无法判定的响应返回 ErrActionOutcomeUnknown。
func (c *Client) SubmitAction(ctx context.Context, form *ActionForm, overrides url.Values) ([]byte, error) {
	if form == nil || strings.TrimSpace(form.Path) == "" {
		return nil, ErrActionFormNotFound
	}
	values := url.Values{}
	for key, list := range form.Fields {
		values[key] = append([]string(nil), list...)
	}
	for key, list := range overrides {
		if len(list) > 0 {
			values[key] = append([]string(nil), list...)
		}
	}
	if err := ctx.Err(); err != nil {
		// 尚未发出请求，可以安全放弃或重试。
		return nil, err
	}
	body, err := c.request(ctx, http.MethodPost, form.Path, values, "application/x-www-form-urlencoded")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// 请求可能已被服务端执行：结果未知，提示刷新核实而非自动重试。
		return nil, fmt.Errorf("submit zentao action form: %w: %v", ErrActionOutcomeUnknown, err)
	}
	if isLoginPage(body) {
		return nil, ErrSessionExpired
	}
	switch actionResult(body) {
	case actionSucceeded:
		return body, nil
	case actionRejected:
		return body, ErrActionRejected
	default:
		return body, ErrActionOutcomeUnknown
	}
}

type actionStatus int

const (
	actionUnknown actionStatus = iota
	actionSucceeded
	actionRejected
)

// actionResult 判定禅道对写操作的响应。禅道对 AJAX 提交返回 JSON；明确的
// 业务失败也可能以带错误提示的页面返回。无法确定成功时不报成功。
func actionResult(body []byte) actionStatus {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		var result struct {
			Result  string `json:"result"`
			Outcome string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(trimmed), &result); err == nil {
			switch strings.ToLower(strings.TrimSpace(result.Result)) {
			case "success":
				return actionSucceeded
			case "fail", "failed", "error", "false":
				return actionRejected
			}
			switch strings.ToLower(strings.TrimSpace(result.Outcome)) {
			case "success", "ok":
				return actionSucceeded
			case "fail", "failed", "error":
				return actionRejected
			}
			// 只有 message 没有 result/status 的 JSON 不足以判定成功或失败。
			return actionUnknown
		}
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{
		`class="alert alert-danger"`, `class='alert alert-danger'`,
		`class="alert-danger"`, `class='alert-danger'`,
		`class="form-error"`, `class='form-error'`,
		`class="text-error"`, `class='text-error'`,
		`class="has-error"`, `class='has-error'`,
	} {
		if strings.Contains(lower, marker) {
			return actionRejected
		}
	}
	return actionUnknown
}

// ParseActionForm 解析操作页 HTML（zentaoctl 存完夹具后用同一份内容校验）。
func ParseActionForm(body []byte, objectType, pageURL, baseURL string) (*ActionForm, error) {
	return parseActionForm(body, objectType, pageURL, baseURL)
}

// parseActionForm 找出 action 属性含 objectType 的表单，收集全部字段的
// 当前值：input 取 value（含 hidden/checkbox），select 取选中项（缺省第一
// 项），textarea 取文本。pageURL 用于在表单未写 action 时回到当前操作页。
func parseActionForm(body []byte, objectType, pageURL, baseURL string) (*ActionForm, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}
	var found *ActionForm
	var targetErr error
	walkHTMLElements(doc, func(node *html.Node) {
		if found != nil || node.Data != "form" {
			return
		}
		actionAttr := strings.ToLower(attr(node, "action"))
		if actionAttr != "" && !strings.Contains(actionAttr, objectType) {
			return
		}
		form := &ActionForm{Fields: url.Values{}}
		collectFormFields(node, form.Fields)
		if len(form.Fields) == 0 {
			return
		}
		raw := strings.TrimSpace(attr(node, "action"))
		if raw == "" {
			// 省略 action 的表单按浏览器语义提交到当前页，保留操作与事项 ID。
			raw = pageURL
		}
		path, err := formTargetPath(raw, pageURL, baseURL)
		if err != nil {
			// 目标越界是配置/安全错误，必须如实上报而不是当作没有表单。
			targetErr = err
			return
		}
		form.Path = path
		found = form
	})
	if found == nil {
		if targetErr != nil {
			return nil, targetErr
		}
		return nil, ErrActionFormNotFound
	}
	return found, nil
}

// formTargetPath 把表单 action 解析为相对 BaseURL 的提交路径，并确认目标
// 仍在当前禅道实例内，防止表单把会话凭据提交到外部地址。
func formTargetPath(raw, pageURL, baseURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", ErrActionFormTarget
	}
	page, err := url.Parse(pageURL)
	if err != nil {
		return "", ErrActionFormTarget
	}
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrActionFormTarget
	}
	target := page.ResolveReference(reference)
	if target.User != nil || target.Host != "" && !strings.EqualFold(target.Host, base.Host) ||
		!strings.EqualFold(target.Scheme, base.Scheme) {
		return "", ErrActionFormTarget
	}
	basePath := strings.TrimSuffix(base.Path, "/")
	if !strings.HasPrefix(target.Path, basePath+"/") {
		return "", ErrActionFormTarget
	}
	path := strings.TrimPrefix(target.Path, basePath+"/")
	if path == "" {
		return "", ErrActionFormTarget
	}
	if target.RawQuery != "" {
		path += "?" + target.RawQuery
	}
	return path, nil
}

// collectFormFields 收集表单控件的名字与当前值。
func collectFormFields(form *html.Node, fields url.Values) {
	walkHTMLElements(form, func(node *html.Node) {
		if node == form {
			return
		}
		name := strings.TrimSpace(attr(node, "name"))
		if name == "" {
			return
		}
		switch node.Data {
		case "input":
			inputType := strings.ToLower(attr(node, "type"))
			switch inputType {
			case "submit", "reset", "button", "image", "file":
				return
			}
			if inputType == "checkbox" || inputType == "radio" {
				// 裸 checked 也是勾选状态；value 缺省按 HTML 规则提交 "on"。
				if !hasAttribute(node, "checked") {
					return
				}
				value, ok := getAttr(node, "value")
				if !ok {
					value = "on"
				}
				fields.Add(name, value)
				return
			}
			fields.Set(name, attr(node, "value"))
		case "select":
			values := selectedValues(node)
			for _, value := range values {
				fields.Add(name, value)
			}
		case "textarea":
			fields.Set(name, textContent(node))
		}
	})
}

// selectedValues 返回 select 的浏览器提交值：显式选中的全部选项；单选且
// 未标明 selected 时，浏览器默认选中第一项。
func selectedValues(selectNode *html.Node) []string {
	var selected, options []string
	walkHTMLElements(selectNode, func(option *html.Node) {
		if option.Data != "option" {
			return
		}
		value, ok := getAttr(option, "value")
		if !ok {
			value = strings.TrimSpace(textContent(option))
		}
		options = append(options, value)
		if hasAttribute(option, "selected") {
			selected = append(selected, value)
		}
	})
	if len(selected) == 0 && !hasAttribute(selectNode, "multiple") && len(options) > 0 {
		return options[:1]
	}
	return selected
}

func hasAttribute(node *html.Node, name string) bool {
	_, ok := getAttr(node, name)
	return ok
}

// walkHTMLElements 先序遍历元素节点。
func walkHTMLElements(root *html.Node, visit func(node *html.Node)) {
	if root.Type == html.ElementNode {
		visit(root)
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkHTMLElements(child, visit)
	}
}

// textContent 拼接节点子树的文本。
func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.TrimSpace(builder.String())
}
