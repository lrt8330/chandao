package zentao

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// startFormPage 是任务开始操作页的夹具：隐藏 token、预填输入、下拉与
// 备注文本域。
const startFormPage = `<!DOCTYPE html>
<html><body>
<form method="post" action="index.php?m=task&f=start&taskID=108" id="startForm">
  <input type="hidden" name="verifyRand" value="token-abc">
  <input type="hidden" name="taskID" value="108">
  <input type="text" name="realStarted" value="2026-11-28 09:00">
  <input type="text" name="consumed" value="0">
  <input type="text" name="left" value="8">
  <select name="afterStart">
    <option value="continue" selected>继续</option>
    <option value="pauseOthers">暂停其他</option>
  </select>
  <textarea name="comment"></textarea>
  <input type="submit" value="确定">
</form>
</body></html>`

// TestClientActionFormRoundTrip 登录后抓取操作表单并提交：隐藏域与预填
// 值回填，overrides 覆盖同名字段。
func TestClientActionFormRoundTrip(t *testing.T) {
	var posted url.Values
	var postedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("m") == "user" && r.URL.Query().Get("f") == "refreshRandom":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "123456")
		case r.Method == http.MethodPost && r.URL.Query().Get("m") == "user" && r.URL.Query().Get("f") == "login":
			http.SetCookie(w, &http.Cookie{Name: "zentaosid", Value: "session", Path: "/biz/"})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"result":"success","locate":"/biz/"}`)
		case r.Method == http.MethodGet && r.URL.Query().Get("m") == "task" && r.URL.Query().Get("f") == "start":
			if got := r.URL.Query().Get("taskID"); got != "108" {
				t.Errorf("taskID = %q, want 108", got)
			}
			fmt.Fprint(w, startFormPage)
		case r.Method == http.MethodPost && r.URL.Query().Get("m") == "task" && r.URL.Query().Get("f") == "start":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			postedPath = r.URL.RequestURI()
			posted = r.PostForm
			fmt.Fprint(w, `<html><body>ok</body></html>`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "tester", "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	form, err := client.FetchActionForm(context.Background(), "task", "start", "108")
	if err != nil {
		t.Fatalf("FetchActionForm: %v", err)
	}
	// 隐藏 token 与预填值都要带上。
	want := map[string]string{
		"verifyRand":  "token-abc",
		"taskID":      "108",
		"realStarted": "2026-11-28 09:00",
		"consumed":    "0",
		"left":        "8",
		"afterStart":  "continue",
		"comment":     "",
	}
	for key, value := range want {
		if got := form.Fields[key]; got != value {
			t.Errorf("form[%q] = %q, want %q", key, got, value)
		}
	}
	if !strings.Contains(form.Path, "m=task") || !strings.Contains(form.Path, "taskID=108") {
		t.Fatalf("form path = %q", form.Path)
	}

	if _, err := client.SubmitAction(context.Background(), form, url.Values{"comment": {"开始执行"}}); err != nil {
		t.Fatalf("SubmitAction: %v", err)
	}
	if !strings.Contains(postedPath, "taskID=108") {
		t.Fatalf("posted path = %q", postedPath)
	}
	if got := posted.Get("comment"); got != "开始执行" {
		t.Errorf("posted comment = %q, want override", got)
	}
	if got := posted.Get("verifyRand"); got != "token-abc" {
		t.Errorf("posted verifyRand = %q, want the hidden token", got)
	}
	if got := posted.Get("left"); got != "8" {
		t.Errorf("posted left = %q, want the prefilled value", got)
	}
}

// TestFetchActionFormRejectsUnknownAction 未知对象/动作直接拒绝，不发起请求。
func TestFetchActionFormRejectsUnknownAction(t *testing.T) {
	client, err := New("https://zentao.example.invalid/biz/", "tester", "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.FetchActionForm(context.Background(), "task", "delete", "1"); err == nil {
		t.Fatal("delete action must be rejected")
	}
	if _, err := client.FetchActionForm(context.Background(), "user", "start", "1"); err == nil {
		t.Fatal("unknown object type must be rejected")
	}
}
