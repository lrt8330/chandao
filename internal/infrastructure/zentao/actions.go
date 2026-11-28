// 禅道事项操作：抓取操作页表单（含隐藏 token 与页面预填值），原样回填
// 后提交——等效于在禅道页面上打开操作弹窗直接点确定。
package zentao

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// ErrActionFormNotFound 操作页里找不到可提交的表单。
var ErrActionFormNotFound = errors.New("zentao action form not found")

// ActionForm 是从禅道操作页解析出的表单：目标路径与全部字段。
type ActionForm struct {
	// Path 是表单的提交地址（相对 BaseURL）。
	Path string
	// Fields 含隐藏域（token 等）、输入框/下拉/文本域的页面预填值。
	Fields map[string]string
}

// FetchActionForm 抓取 objectType 的 action 操作页并解析表单。
// 页面地址形如 index.php?m=task&f=start&taskID=123。
func (c *Client) FetchActionForm(ctx context.Context, objectType, action, id string) (*ActionForm, error) {
	switch objectType {
	case "task", "bug", "story":
	default:
		return nil, ErrInvalidPagination
	}
	switch action {
	case "start", "finish", "pause", "cancel", "close", "activate", "resolve", "confirm", "assign", "transfer", "recordEstimate":
	default:
		return nil, ErrInvalidPagination
	}
	path := fmt.Sprintf("index.php?m=%s&f=%s&%sID=%s", objectType, action, objectType, url.QueryEscape(id))
	body, err := c.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, fmt.Errorf("fetch zentao %s %s page: %w", objectType, action, err)
	}
	if isLoginPage(body) {
		return nil, ErrSessionExpired
	}
	form, err := parseActionForm(body, objectType)
	if err != nil {
		return nil, fmt.Errorf("parse zentao %s %s form: %w", objectType, action, err)
	}
	return form, nil
}

// SubmitAction 提交操作表单。overrides 里的字段覆盖页面预填值（如备注）。
// 返回提交后的响应体（调用方据此判断成败，登录页说明会话失效）。
func (c *Client) SubmitAction(ctx context.Context, form *ActionForm, overrides url.Values) ([]byte, error) {
	if form == nil || strings.TrimSpace(form.Path) == "" {
		return nil, ErrActionFormNotFound
	}
	values := url.Values{}
	for key, value := range form.Fields {
		values.Set(key, value)
	}
	for key, list := range overrides {
		for _, value := range list {
			values.Set(key, value)
		}
	}
	body, err := c.request(ctx, http.MethodPost, form.Path, values, "application/x-www-form-urlencoded")
	if err != nil {
		return nil, fmt.Errorf("submit zentao action form: %w", err)
	}
	if isLoginPage(body) {
		return nil, ErrSessionExpired
	}
	return body, nil
}

// parseActionForm 找出 action 属性含 objectType 的表单，收集全部字段的
// 当前值：input 取 value（含 hidden/checkbox），select 取选中项（缺省第一
// 项），textarea 取文本。
func parseActionForm(body []byte, objectType string) (*ActionForm, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}
	var found *ActionForm
	walkHTMLElements(doc, func(node *html.Node) {
		if found != nil || node.Data != "form" {
			return
		}
		actionAttr := strings.ToLower(attr(node, "action"))
		if actionAttr != "" && !strings.Contains(actionAttr, objectType) {
			return
		}
		form := &ActionForm{Fields: map[string]string{}}
		if raw := strings.TrimSpace(attr(node, "action")); raw != "" {
			form.Path = raw
		}
		collectFormFields(node, form.Fields)
		if len(form.Fields) > 0 {
			found = form
		}
	})
	if found == nil {
		return nil, ErrActionFormNotFound
	}
	if found.Path == "" {
		// 无 action 属性的表单按禅道习惯提交到当前页对应的模块方法。
		found.Path = "index.php"
	}
	return found, nil
}

// collectFormFields 收集表单控件的名字与当前值。
func collectFormFields(form *html.Node, fields map[string]string) {
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
			if inputType == "submit" || inputType == "button" || inputType == "image" || inputType == "file" {
				return
			}
			if (inputType == "checkbox" || inputType == "radio") && attr(node, "checked") == "" {
				return
			}
			fields[name] = attr(node, "value")
		case "select":
			selected := ""
			fallback := ""
			walkHTMLElements(node, func(option *html.Node) {
				if option.Data != "option" {
					return
				}
				value := attr(option, "value")
				if fallback == "" {
					fallback = value
				}
				if selected == "" && attr(option, "selected") != "" {
					selected = value
				}
			})
			if selected == "" {
				selected = fallback
			}
			fields[name] = selected
		case "textarea":
			fields[name] = textContent(node)
		}
	})
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
