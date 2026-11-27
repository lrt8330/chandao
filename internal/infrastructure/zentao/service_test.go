package zentao

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"chandao/zentaotray/internal/application"
)

func TestServiceAggregatesAllTaskAndBugPages(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageID := r.URL.Query().Get("pageID")
		if got := r.URL.Query().Get("recPerPage"); got != "100" {
			t.Errorf("recPerPage = %q, want 100", got)
		}
		mu.Lock()
		requests = append(requests, r.URL.Path+"?pageID="+pageID)
		mu.Unlock()

		switch r.URL.Path {
		case "/biz/my-task-type=assignedTo.html":
			if pageID == "1" {
				fmt.Fprint(w, serviceAssignedPage("task", 2, serviceTestItem{ID: "102", Title: "第二页任务", Status: "wait"}))
				return
			}
			if pageID == "2" {
				fmt.Fprint(w, serviceAssignedPage("task", 1, serviceTestItem{ID: "101", Title: "第一页任务", Status: "doing"}))
				return
			}
		case "/biz/my-bug-type=assignedTo.html":
			if pageID == "1" {
				fmt.Fprint(w, serviceAssignedPage("bug", 2, serviceTestItem{ID: "202", Title: "第二页缺陷", Status: "active"}))
				return
			}
			if pageID == "2" {
				fmt.Fprint(w, serviceAssignedPage("bug", 1, serviceTestItem{ID: "201", Title: "第一页缺陷", Status: "active"}))
				return
			}
		}
		t.Errorf("unexpected request: %s", r.URL.String())
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := NewService(client).FetchAssignedItems(context.Background())
	if err != nil {
		t.Fatalf("FetchAssignedItems: %v", err)
	}
	var keys []string
	for _, item := range items {
		keys = append(keys, item.Key())
	}
	wantKeys := []string{"bug:201", "bug:202", "task:101", "task:102"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("item keys = %v, want %v", keys, wantKeys)
	}
	mu.Lock()
	defer mu.Unlock()
	wantRequests := []string{
		"/biz/my-task-type=assignedTo.html?pageID=1",
		"/biz/my-task-type=assignedTo.html?pageID=2",
		"/biz/my-bug-type=assignedTo.html?pageID=1",
		"/biz/my-bug-type=assignedTo.html?pageID=2",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("requests = %v, want %v", requests, wantRequests)
	}
}

func TestServiceDiscardsPartialSnapshotOnLaterFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/biz/my-task-type=assignedTo.html":
			fmt.Fprint(w, serviceAssignedPage("task", 1, serviceTestItem{ID: "101", Title: "完整任务", Status: "wait"}))
		case "/biz/my-bug-type=assignedTo.html":
			if r.URL.Query().Get("pageID") == "1" {
				fmt.Fprint(w, serviceAssignedPage("bug", 2, serviceTestItem{ID: "201", Title: "部分缺陷", Status: "active"}))
				return
			}
			http.Error(w, "failure", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := NewService(client).FetchAssignedItems(context.Background())
	if items != nil {
		t.Fatalf("items = %#v, want nil after a later page fails", items)
	}
	if err == nil || !strings.Contains(err.Error(), "HTTP status 500") {
		t.Fatalf("error = %v, want the later page failure", err)
	}
}

func TestServiceStopsAfterMaximumPageCount(t *testing.T) {
	var taskRequests atomic.Int32
	var bugRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/biz/my-task-type=assignedTo.html" {
			taskRequests.Add(1)
			fmt.Fprint(w, serviceAssignedPage("task", 2, serviceTestItem{ID: "101", Title: "持续分页的任务", Status: "wait"}))
			return
		}
		if r.URL.Path == "/biz/my-bug-type=assignedTo.html" {
			bugRequests.Add(1)
			fmt.Fprint(w, serviceAssignedPage("bug", 0))
			return
		}
		t.Errorf("unexpected request: %s", r.URL.String())
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := NewService(client).FetchAssignedItems(context.Background())
	if items != nil {
		t.Fatalf("items = %#v, want nil when the page guard is reached", items)
	}
	if !errors.Is(err, ErrPageLimitExceeded) || !strings.Contains(err.Error(), "100") {
		t.Fatalf("error = %v, want an explicit 100-page limit error", err)
	}
	if got := taskRequests.Load(); got != 100 {
		t.Errorf("task requests = %d, want 100", got)
	}
	if got := bugRequests.Load(); got != 0 {
		t.Errorf("bug requests = %d, want 0", got)
	}
}

func TestServiceIgnoresIdenticalDuplicateItems(t *testing.T) {
	item := serviceTestItem{ID: "101", Title: "重复出现的相同任务", Status: "wait"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/biz/my-task-type=assignedTo.html":
			total := 1
			if r.URL.Query().Get("pageID") == "1" {
				total = 2
			}
			fmt.Fprint(w, serviceAssignedPage("task", total, item))
		case "/biz/my-bug-type=assignedTo.html":
			fmt.Fprint(w, serviceAssignedPage("bug", 0))
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := NewService(client).FetchAssignedItems(context.Background())
	if err != nil {
		t.Fatalf("FetchAssignedItems: %v", err)
	}
	if len(items) != 1 || items[0].Key() != "task:101" {
		t.Fatalf("items = %#v, want one identical task", items)
	}
}

func TestServiceRejectsConflictingDuplicateItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/biz/my-bug-type=assignedTo.html" {
			fmt.Fprint(w, serviceAssignedPage("bug", 0))
			return
		}
		if r.URL.Path != "/biz/my-task-type=assignedTo.html" {
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("pageID") == "1" {
			fmt.Fprint(w, serviceAssignedPage("task", 2, serviceTestItem{ID: "101", Title: "任务版本一", Status: "wait"}))
			return
		}
		fmt.Fprint(w, serviceAssignedPage("task", 1, serviceTestItem{ID: "101", Title: "任务版本二", Status: "doing"}))
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := NewService(client).FetchAssignedItems(context.Background())
	if items != nil {
		t.Fatalf("items = %#v, want nil for conflicting duplicates", items)
	}
	if !errors.Is(err, ErrConflictingDuplicate) || !strings.Contains(err.Error(), "task:101") {
		t.Fatalf("error = %v, want a clear task:101 conflict", err)
	}
}

func TestServiceMapsSessionExpiryAndDiscardsTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/biz/my-task-type=assignedTo.html":
			fmt.Fprint(w, serviceAssignedPage("task", 1, serviceTestItem{ID: "101", Title: "已获取任务", Status: "wait"}))
		case "/biz/my-bug-type=assignedTo.html":
			fmt.Fprint(w, `<html><form method="post"><input name="account"><input type="password" name="password"></form></html>`)
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "test-user", "test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := NewService(client).FetchAssignedItems(context.Background())
	if items != nil {
		t.Fatalf("items = %#v, want nil on session expiry", items)
	}
	if !errors.Is(err, application.ErrSessionExpired) {
		t.Fatalf("error = %v, want application.ErrSessionExpired", err)
	}
}

type serviceTestItem struct {
	ID     string
	Title  string
	Status string
}

func serviceAssignedPage(itemType string, total int, items ...serviceTestItem) string {
	tableID := "tasktable"
	if itemType == "bug" {
		tableID = "bugList"
	}
	var body string
	for _, item := range items {
		body += fmt.Sprintf(`<tr data-id="%s"><td class="c-name"><a href="/biz/%s-view-%s.html">%s</a></td><td class="c-status">%s</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>`,
			item.ID, itemType, item.ID, item.Title, item.Status)
	}
	return fmt.Sprintf(`<input id="recTotal" value="%d"><input id="recPerPage" value="100"><table id="%s"><tbody>%s</tbody></table>`, total, tableID, body)
}
