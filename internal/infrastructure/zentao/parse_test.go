package zentao

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"chandao/zentaotray/internal/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	assignedTasksPageURL = "https://zentao.example.invalid/biz/my-task-type=assignedTo.html"
	assignedBugsPageURL  = "https://zentao.example.invalid/biz/my-bug-type=assignedTo.html"
)

func TestParseAssignedTasksModern(t *testing.T) {
	body := readAssignedFixture(t, "assigned_tasks_modern.html")
	got, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	want := []domain.Item{
		{
			ID:        "101",
			Type:      domain.ItemTask,
			Title:     "修复&验证 <登录流程>",
			Project:   "示例项目",
			Status:    "doing",
			UpdatedAt: "2026-11-27 10:30:00",
			URL:       "https://zentao.example.invalid/biz/task-view-101.html",
		},
		{
			ID:        "102",
			Type:      domain.ItemTask,
			Title:     "整理测试记录",
			Project:   "另一个示例项目",
			Status:    "wait",
			UpdatedAt: "2026-11-26",
			URL:       "https://zentao.example.invalid/biz/task-view.html?taskID=102",
		},
		{
			ID:        "103",
			Type:      domain.ItemTask,
			Title:     "暂停中的任务",
			Project:   "示例项目",
			Status:    "pause",
			UpdatedAt: "2026-11-25",
			URL:       "https://zentao.example.invalid/biz/task-view-103.html",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %#v, want %#v", got, want)
	}
}

func TestParseAssignedBugsModern(t *testing.T) {
	body := readAssignedFixture(t, "assigned_bugs_modern.html")
	got, err := ParseAssignedBugs(body, assignedBugsPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedBugs: %v", err)
	}
	want := []domain.Item{
		{
			ID:        "201",
			Type:      domain.ItemBug,
			Title:     "示例页面&提示重复",
			Project:   "示例项目",
			Status:    "active",
			UpdatedAt: "2026-11-27 11:20:00",
			URL:       "https://zentao.example.invalid/biz/bug-view-201.html",
		},
		{
			ID:        "203",
			Type:      domain.ItemBug,
			Title:     "待验证的边界问题",
			Project:   "示例项目",
			Status:    "active",
			UpdatedAt: "2026-11-25",
			URL:       "https://zentao.example.invalid/biz/bug-view-203.html",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %#v, want %#v", got, want)
	}
}

func TestParseAssignedTasksLegacy(t *testing.T) {
	body := readAssignedFixture(t, "assigned_tasks_legacy.html")
	got, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	want := []domain.Item{{
		ID:        "301",
		Type:      domain.ItemTask,
		Title:     "核对旧版列表",
		Project:   "示例项目",
		Status:    "wait",
		UpdatedAt: "2026-11-27 09:00:00",
		URL:       "https://zentao.example.invalid/biz/task-view-301.html",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %#v, want %#v", got, want)
	}
}

func TestParseAssignedBugsLegacy(t *testing.T) {
	body := readAssignedFixture(t, "assigned_bugs_legacy.html")
	got, err := ParseAssignedBugs(body, assignedBugsPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedBugs: %v", err)
	}
	want := []domain.Item{{
		ID:        "401",
		Type:      domain.ItemBug,
		Title:     "旧版列表中的示例缺陷",
		Project:   "示例项目",
		Status:    "active",
		UpdatedAt: "2026-11-27 08:45:00",
		URL:       "https://zentao.example.invalid/biz/bug-view-401.html",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %#v, want %#v", got, want)
	}
}

func TestParseAssignedTasksIgnoresNonDetailActionLinks(t *testing.T) {
	body := []byte(`<table id="tasktable"><tbody><tr data-id="1601">
<td class="c-actions"><a href="/biz/task-view-1601-edit.html">编辑</a></td>
<td class="c-name"><a href="/biz/task-view-1601.html">真正的任务标题</a></td>
<td class="c-status">wait</td><td class="c-project">示例项目</td>
<td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if len(items) != 1 || items[0].Title != "真正的任务标题" {
		t.Fatalf("items = %#v, want the real detail link", items)
	}
}

func TestParseAssignedTasksRejectsIDContainerWithoutTable(t *testing.T) {
	body := []byte(`<div id="tasktable"><p>unknown structure</p></div>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if !errors.Is(err, ErrUnexpectedStructure) {
		t.Fatalf("items = %#v, error = %v, want ErrUnexpectedStructure", items, err)
	}
}

func TestParseAssignedTasksModernTableInsideIDContainer(t *testing.T) {
	body := []byte(`<div id="tasktable"><table><tbody><tr data-id="1501">
<td class="c-name"><a href="/biz/task-view-1501.html">容器内任务</a></td>
<td class="c-status">wait</td><td class="c-project">示例项目</td>
<td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table></div>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if len(items) != 1 || items[0].ID != "1501" {
		t.Fatalf("items = %#v, want row scoped under the ID container", items)
	}
}

func TestParseAssignedTasksDoesNotUseUnscopedLegacyTableWhenModernTableExists(t *testing.T) {
	body := []byte(`<table id="tasktable"></table>
<table><thead><tr><th>ID</th><th>Name</th><th>Status</th><th>Project</th><th>Last Edited</th></tr></thead>
<tbody><tr><td>1401</td><td><a href="/biz/task-view-1401.html">不应解析的任务</a></td><td>wait</td><td>示例项目</td><td>2026-11-27</td></tr></tbody></table>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want the recognized modern table to remain empty", items)
	}
}

func TestParseAssignedTasksLegacyHeaderInTbody(t *testing.T) {
	body := []byte(`<table><tbody>
<tr><th>状态</th><th>ID</th><th>任务名称</th><th>项目</th><th>最后编辑</th></tr>
<tr><td><span class="status-doing">doing</span></td><td>1402</td><td><a href="/biz/task-view-1402.html">表体表头任务</a></td><td>示例项目</td><td>2026-11-27</td></tr>
</tbody></table>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if len(items) != 1 || items[0].ID != "1402" || items[0].Status != "doing" {
		t.Fatalf("items = %#v, want legacy tbody header mapping", items)
	}
}

func TestParseAssignedTasksAllowsDefaultPortEquivalentOrigin(t *testing.T) {
	body := []byte(`<table id="tasktable"><tbody><tr data-id="1301">
<td class="c-name"><a href="https://zentao.example.invalid/biz/task-view-1301.html">默认端口任务</a></td>
<td class="c-status">wait</td><td class="c-project">示例项目</td>
<td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table>`)
	items, err := ParseAssignedTasks(body, "https://zentao.example.invalid:443/biz/my-task-type=assignedTo.html")
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if len(items) != 1 || items[0].URL != "https://zentao.example.invalid/biz/task-view-1301.html" {
		t.Fatalf("items = %#v, want same-origin default-port URL", items)
	}
}

func TestParseAssignedTasksValidatesPageURLForEmptyTable(t *testing.T) {
	tests := []struct {
		name    string
		pageURL string
	}{
		{name: "relative", pageURL: "/biz/my-task-type=assignedTo.html"},
		{name: "outside biz", pageURL: "https://zentao.example.invalid/my-task-type=assignedTo.html"},
		{name: "unsupported scheme", pageURL: "ftp://zentao.example.invalid/biz/my-task-type=assignedTo.html"},
		{name: "credentials", pageURL: "https://user:secret@zentao.example.invalid/biz/my-task-type=assignedTo.html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := ParseAssignedTasks([]byte(`<table id="tasktable"></table>`), tt.pageURL)
			if !errors.Is(err, ErrInvalidDetailURL) {
				t.Fatalf("items = %#v, error = %v, want ErrInvalidDetailURL", items, err)
			}
		})
	}
}

func TestParseAssignedTasksStatusIgnoresActionsAndTranslations(t *testing.T) {
	tests := []struct {
		name       string
		rowStatus  string
		statusCell string
		action     string
		wantStatus string
		wantErr    bool
	}{
		{name: "translated label", statusCell: `<td class="c-status">已完成</td>`, wantErr: true},
		{name: "action button only", statusCell: `<td class="c-status"></td>`, action: `<a class="btn" href="/biz/task-view-1201-edit.html">完成</a>`, wantErr: true},
		{name: "unknown raw code", statusCell: `<td class="c-status">mystery</td>`, wantErr: true},
		{name: "data status wins", rowStatus: ` data-status="wait"`, statusCell: `<td class="c-status"></td>`, action: `<a class="btn" href="/biz/task-view-1201-edit.html">完成</a>`, wantStatus: "wait"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`<table id="tasktable"><tbody><tr data-id="1201"` + tt.rowStatus + `>` +
				`<td class="c-name"><a href="/biz/task-view-1201.html">状态来源测试</a></td>` +
				tt.statusCell + `<td class="c-project">示例项目</td>` +
				`<td class="c-lastEditedDate">2026-11-27</td><td class="c-actions">` + tt.action + `</td>` +
				`</tr></tbody></table>`)
			items, err := ParseAssignedTasks(body, assignedTasksPageURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("items = %#v, want an error rather than a guessed status", items)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAssignedTasks: %v", err)
			}
			if len(items) != 1 || items[0].Status != tt.wantStatus {
				t.Fatalf("items = %#v, want status %q", items, tt.wantStatus)
			}
		})
	}
}

func TestParseAssignedTasksUsesRowStatusData(t *testing.T) {
	body := []byte(`<table id="tasktable"><tbody><tr data-id="1102" data-status="pause">
<td class="c-name"><a href="/biz/task-view-1102.html">行级状态任务</a></td>
<td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td>
</tr></tbody></table>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if len(items) != 1 || items[0].Status != "pause" {
		t.Fatalf("items = %#v, want raw row status", items)
	}
}

func TestParseAssignedTasksFallsBackToTitleCell(t *testing.T) {
	body := []byte(`<table id="tasktable"><tbody><tr data-id="1101">
<td class="c-name"><a href="/biz/task-view-1101.html"></a></td>
<td class="c-title">  备用&amp;标题  </td>
<td class="c-status">wait</td><td class="c-project">示例项目</td>
<td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks: %v", err)
	}
	if len(items) != 1 || items[0].Title != "备用&标题" {
		t.Fatalf("items = %#v, want title-cell fallback", items)
	}
}

func TestParseAssignedTasksRequiresPaginationUnlessAllRowsArePresent(t *testing.T) {
	const firstRow = `<tr data-id="1001"><td class="c-name"><a href="/biz/task-view-1001.html">第一页任务</a></td><td class="c-status">wait</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>`
	const secondRow = `<tr data-id="1002"><td class="c-name"><a href="/biz/task-view-1002.html">第二页任务</a></td><td class="c-status">doing</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>`
	t.Run("missing rows", func(t *testing.T) {
		body := []byte(`<input id="recTotal" value="2"><input id="recPerPage" value="1"><table id="tasktable"><tbody>` + firstRow + `</tbody></table>`)
		items, err := ParseAssignedTasks(body, assignedTasksPageURL)
		if !errors.Is(err, ErrPaginationRequired) {
			t.Fatalf("items = %#v, error = %v, want ErrPaginationRequired", items, err)
		}
		if len(items) != 1 || items[0].ID != "1001" {
			t.Fatalf("items = %#v, want the row parsed before pagination was required", items)
		}
	})
	t.Run("bugs missing rows retain parsed active items", func(t *testing.T) {
		body := []byte(`<input id="recTotal" value="3"><input id="recPerPage" value="1"><table id="bugList"><tbody>
		<tr data-id="2001"><td class="c-title"><a href="/biz/bug-view-2001.html">第一页缺陷</a></td><td class="c-status">active</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>
		<tr data-id="2002"><td class="c-title"><a href="/biz/bug-view-2002.html">已解决缺陷</a></td><td class="c-status">resolved</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>
		</tbody></table>`)
		items, err := ParseAssignedBugs(body, assignedBugsPageURL)
		if !errors.Is(err, ErrPaginationRequired) {
			t.Fatalf("items = %#v, error = %v, want ErrPaginationRequired", items, err)
		}
		if len(items) != 1 || items[0].ID != "2001" {
			t.Fatalf("items = %#v, want only the parsed active bug", items)
		}
	})
	t.Run("all rows present", func(t *testing.T) {
		body := []byte(`<input id="recTotal" value="2"><input id="recPerPage" value="1"><table id="tasktable"><tbody>` + firstRow + secondRow + `</tbody></table>`)
		items, err := ParseAssignedTasks(body, assignedTasksPageURL)
		if err != nil {
			t.Fatalf("ParseAssignedTasks: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("items = %#v, want both rows", items)
		}
	})
}

func TestParseAssignedTasksPaginationUsesPageContext(t *testing.T) {
	const firstRow = `<tr data-id="1001"><td class="c-name"><a href="/biz/task-view-1001.html">第一页任务</a></td><td class="c-status">wait</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>`
	const secondRow = `<tr data-id="1002"><td class="c-name"><a href="/biz/task-view-1002.html">第二页任务</a></td><td class="c-status">doing</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>`
	tests := []struct {
		name           string
		total          int
		metadataSize   int
		query          string
		rows           string
		wantIDs        []string
		wantErr        error
		wantErrText    string
		wantNilOnError bool
	}{
		{
			name:         "full first page requires another page",
			total:        3,
			metadataSize: 2,
			query:        "pageID=1&recPerPage=2",
			rows:         firstRow + secondRow,
			wantIDs:      []string{"1001", "1002"},
			wantErr:      ErrPaginationRequired,
		},
		{
			name:         "partial final page completes collection",
			total:        3,
			metadataSize: 2,
			query:        "pageID=2&recPerPage=2",
			rows:         secondRow,
			wantIDs:      []string{"1002"},
		},
		{
			name:         "constant total completes on short final page",
			total:        2,
			metadataSize: 100,
			query:        "pageID=2&recPerPage=100",
			rows:         secondRow,
			wantIDs:      []string{"1002"},
		},
		{
			name:         "page beyond total is cleanly complete",
			total:        2,
			metadataSize: 2,
			query:        "pageID=3&recPerPage=2",
			wantIDs:      []string{},
		},
		{
			name:           "incomplete middle page is an error",
			total:          5,
			metadataSize:   2,
			query:          "pageID=2&recPerPage=2",
			rows:           secondRow,
			wantErr:        ErrIncompletePage,
			wantErrText:    "page 2 expected 2 rows, got 1",
			wantNilOnError: true,
			wantIDs:        []string{},
		},
		{
			name:           "one row cannot fill an oversized first page",
			total:          2,
			metadataSize:   100,
			query:          "pageID=1&recPerPage=100",
			rows:           firstRow,
			wantErr:        ErrIncompletePage,
			wantErrText:    "page 1 expected 2 rows, got 1",
			wantNilOnError: true,
			wantIDs:        []string{},
		},
		{
			name:           "query page size drives expected rows",
			total:          5,
			metadataSize:   100,
			query:          "pageID=2&recPerPage=2",
			rows:           secondRow,
			wantErr:        ErrIncompletePage,
			wantErrText:    "page 2 expected 2 rows, got 1",
			wantNilOnError: true,
			wantIDs:        []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`<input id="recTotal" value="%d"><input id="recPerPage" value="%d"><table id="tasktable"><tbody>%s</tbody></table>`, tt.total, tt.metadataSize, tt.rows))
			items, err := ParseAssignedTasks(body, assignedTasksPageURL+"?"+tt.query)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("items = %#v, error = %v, want %v", items, err, tt.wantErr)
			}
			if tt.wantErr == nil && err != nil {
				t.Fatalf("ParseAssignedTasks: %v", err)
			}
			if tt.wantErrText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrText)) {
				t.Fatalf("error = %v, want text %q", err, tt.wantErrText)
			}
			if errors.Is(err, ErrIncompletePage) && errors.Is(err, ErrPaginationRequired) {
				t.Fatalf("error = %v, incomplete pages must not request another page", err)
			}
			if tt.wantNilOnError && items != nil {
				t.Fatalf("items = %#v, want nil for an incomplete page", items)
			}
			if tt.wantErr == nil && items == nil {
				t.Fatal("items = nil, want a non-nil slice on success")
			}
			gotIDs := make([]string, 0, len(items))
			for _, item := range items {
				gotIDs = append(gotIDs, item.ID)
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Fatalf("item IDs = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestParseAssignedTasksRejectsInvalidPaginationQuery(t *testing.T) {
	body := []byte(`<input id="recTotal" value="2"><input id="recPerPage" value="100"><table id="tasktable"><tbody><tr data-id="1001"><td class="c-name"><a href="/biz/task-view-1001.html">分页查询测试</a></td><td class="c-status">wait</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table>`)
	tests := []struct {
		name  string
		query string
	}{
		{name: "zero page ID", query: "pageID=0&recPerPage=100"},
		{name: "negative page ID", query: "pageID=-1&recPerPage=100"},
		{name: "nonnumeric page ID", query: "pageID=abc&recPerPage=100"},
		{name: "empty page ID", query: "pageID=&recPerPage=100"},
		{name: "repeated page ID", query: "pageID=1&pageID=2&recPerPage=100"},
		{name: "overflowing page ID", query: "pageID=9223372036854775808&recPerPage=100"},
		{name: "zero page size", query: "pageID=1&recPerPage=0"},
		{name: "negative page size", query: "pageID=1&recPerPage=-1"},
		{name: "nonnumeric page size", query: "pageID=1&recPerPage=abc"},
		{name: "empty page size", query: "pageID=1&recPerPage="},
		{name: "repeated page size", query: "pageID=1&recPerPage=100&recPerPage=100"},
		{name: "overflowing page size", query: "pageID=1&recPerPage=9223372036854775808"},
		{name: "malformed query escape", query: "pageID=%zz&recPerPage=100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := ParseAssignedTasks(body, assignedTasksPageURL+"?"+tt.query)
			if !errors.Is(err, ErrInvalidPagination) {
				t.Fatalf("items = %#v, error = %v, want ErrInvalidPagination", items, err)
			}
			if items != nil {
				t.Fatalf("items = %#v, want nil for invalid pagination", items)
			}
		})
	}
}

func TestHasMorePagesReadsPaginationMetadata(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    bool
		wantErr bool
	}{
		{name: "no metadata", body: `<table id="tasktable"></table>`, want: false},
		{name: "single page", body: `<input id="recTotal" value="1"><input id="recPerPage" value="20">`, want: false},
		{name: "more pages", body: `<input id="recTotal" value="21"><input id="recPerPage" value="20">`, want: true},
		{name: "class markers", body: `<span class="recTotal">41</span><span class="recPerPage">20</span>`, want: true},
		{name: "data attributes", body: `<table data-rec-total="2" data-rec-per-page="1"></table>`, want: true},
		{name: "partial metadata", body: `<input id="recTotal" value="2">`, wantErr: true},
		{name: "malformed metadata", body: `<input id="recTotal" value="many"><input id="recPerPage" value="20">`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HasMorePages([]byte(tt.body))
			if tt.wantErr {
				if !errors.Is(err, ErrUnexpectedStructure) {
					t.Fatalf("HasMorePages error = %v, want ErrUnexpectedStructure", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("HasMorePages: %v", err)
			}
			if got != tt.want {
				t.Fatalf("HasMorePages = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestParseAssignedTasksCharacterEncodings(t *testing.T) {
	const markup = `<html><head><meta http-equiv="Content-Type" content="text/html; charset=GB18030"></head><body>
<table id="tasktable"><tbody><tr data-id="901">
<td class="c-name"><a href="/biz/task-view-901.html">编码测试任务</a></td>
<td class="c-status">wait</td><td class="c-project">示例项目</td>
<td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table></body></html>`
	gb18030, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(markup))
	if err != nil {
		t.Fatalf("encode GB18030 fixture: %v", err)
	}
	if utf8.Valid(gb18030) {
		t.Fatal("GB18030 fixture unexpectedly contains valid UTF-8")
	}
	items, err := ParseAssignedTasks(gb18030, assignedTasksPageURL)
	if err != nil {
		t.Fatalf("ParseAssignedTasks GB18030: %v", err)
	}
	if len(items) != 1 || items[0].Title != "编码测试任务" || items[0].Project != "示例项目" {
		t.Fatalf("items = %#v, want decoded GB18030 title and project", items)
	}
}

func TestParseAssignedTasksEncodingDetectionIsConservative(t *testing.T) {
	const rowTemplate = `<table id="tasktable"><tbody><tr data-id="902"><td class="c-name"><a href="/biz/task-view-902.html">%s</a></td><td class="c-status">wait</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table>`
	t.Run("valid UTF-8 wins over a stale declaration", func(t *testing.T) {
		body := []byte(`<meta charset="GB18030">` + strings.Replace(rowTemplate, "%s", "中文任务", 1))
		items, err := ParseAssignedTasks(body, assignedTasksPageURL)
		if err != nil {
			t.Fatalf("ParseAssignedTasks: %v", err)
		}
		if len(items) != 1 || items[0].Title != "中文任务" {
			t.Fatalf("items = %#v, want valid UTF-8 title", items)
		}
	})
	t.Run("undeclared invalid UTF-8 is rejected", func(t *testing.T) {
		body := append([]byte(strings.Replace(rowTemplate, "%s", "任务", 1)), 0xff)
		items, err := ParseAssignedTasks(body, assignedTasksPageURL)
		if !errors.Is(err, ErrUnexpectedStructure) {
			t.Fatalf("items = %#v, error = %v, want ErrUnexpectedStructure", items, err)
		}
	})
	t.Run("invalid GB18030 sequence is rejected", func(t *testing.T) {
		body := []byte(`<meta charset="GB18030">` + strings.Replace(rowTemplate, "%s", "任务", 1))
		body = bytes.Replace(body, []byte("任务"), []byte{0x81, 0x30}, 1)
		items, err := ParseAssignedTasks(body, assignedTasksPageURL)
		if !errors.Is(err, ErrUnexpectedStructure) {
			t.Fatalf("items = %#v, error = %v, want ErrUnexpectedStructure", items, err)
		}
	})
}

func TestParseAssignedRejectsUnknownStructure(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		parse func([]byte, string) ([]domain.Item, error)
		url   string
	}{
		{name: "empty task response", body: "", parse: ParseAssignedTasks, url: assignedTasksPageURL},
		{name: "dashboard task response", body: `<html><body><h1>我的工作台</h1></body></html>`, parse: ParseAssignedTasks, url: assignedTasksPageURL},
		{name: "unrelated bug table", body: `<table><tbody><tr><td>unrelated data</td></tr></tbody></table>`, parse: ParseAssignedBugs, url: assignedBugsPageURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := tt.parse([]byte(tt.body), tt.url)
			if !errors.Is(err, ErrUnexpectedStructure) {
				t.Fatalf("items = %#v, error = %v, want ErrUnexpectedStructure", items, err)
			}
			if items != nil {
				t.Fatalf("items = %#v, want nil", items)
			}
		})
	}
}

func TestParseAssignedBugsLegacyWithoutStatusReturnsClearError(t *testing.T) {
	body := []byte(`<table><thead><tr><th>Bug ID</th><th>Bug 标题</th><th>项目</th></tr></thead>
<tbody><tr><td>801</td><td><a href="/biz/bug-view-801.html">缺少状态的示例 Bug</a></td><td>示例项目</td></tr></tbody></table>`)
	items, err := ParseAssignedBugs(body, assignedBugsPageURL)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "status") {
		t.Fatalf("items = %#v, error = %v, want a clear missing-status error", items, err)
	}
	if items != nil {
		t.Fatalf("items = %#v, want nil", items)
	}
}

func TestParseAssignedBugsEmptyLegacyTableWithoutStatusReturnsClearError(t *testing.T) {
	body := []byte(`<table><thead><tr><th>Bug ID</th><th>Bug 标题</th><th>项目</th></tr></thead><tbody></tbody></table>`)
	items, err := ParseAssignedBugs(body, assignedBugsPageURL)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "status") {
		t.Fatalf("items = %#v, error = %v, want a clear missing-status error", items, err)
	}
}

func TestParseAssignedTasksRejectsInvalidDetailURLs(t *testing.T) {
	tests := []struct {
		name string
		href string
	}{
		{name: "malformed escape", href: `/biz/task-view-%zz.html`},
		{name: "external origin", href: `https://external.example.invalid/biz/task-view-701.html`},
		{name: "different port", href: `https://zentao.example.invalid:444/biz/task-view-701.html`},
		{name: "protocol relative external", href: `//external.example.invalid/biz/task-view-701.html`},
		{name: "wrong object type", href: `/biz/bug-view-701.html`},
		{name: "outside biz prefix", href: `/task-view-701.html`},
		{name: "javascript scheme", href: `javascript:/biz/task-view-701.html`},
		{name: "file scheme", href: `file:///biz/task-view-701.html`},
		{name: "zero ID", href: `/biz/task-view-0.html`},
		{name: "negative ID", href: `/biz/task-view--1.html`},
		{name: "nonnumeric ID", href: `/biz/task-view-abc.html`},
		{name: "signed ID", href: `/biz/task-view-+1.html`},
		{name: "overflowing ID", href: `/biz/task-view-18446744073709551616.html`},
		{name: "missing query ID", href: `/biz/task-view.html`},
		{name: "wrong query key", href: `/biz/task-view.html?bugID=701`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`<table id="tasktable"><tbody><tr data-id="701">` +
				`<td class="c-name"><a href="` + tt.href + `">示例任务</a></td>` +
				`<td class="c-status">wait</td><td class="c-project">示例项目</td>` +
				`<td class="c-lastEditedDate">2026-11-27</td></tr></tbody></table>`)
			items, err := ParseAssignedTasks(body, assignedTasksPageURL)
			if !errors.Is(err, ErrInvalidDetailURL) {
				t.Fatalf("items = %#v, error = %v, want ErrInvalidDetailURL", items, err)
			}
			if items != nil {
				t.Fatalf("items = %#v, want nil", items)
			}
		})
	}
}

func TestParseAssignedTasksRejectsMissingFields(t *testing.T) {
	const row = `<tr data-id="601">
<td class="c-name"><a href="/biz/task-view-601.html">示例任务</a></td>
<td class="c-status">wait</td>
<td class="c-project">示例项目</td>
<td class="c-lastEditedDate">2026-11-27</td>
</tr>`
	tests := []struct {
		name   string
		remove string
	}{
		{name: "title", remove: `<td class="c-name"><a href="/biz/task-view-601.html">示例任务</a></td>`},
		{name: "project", remove: `<td class="c-project">示例项目</td>`},
		{name: "status", remove: `<td class="c-status">wait</td>`},
		{name: "updated date", remove: `<td class="c-lastEditedDate">2026-11-27</td>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`<table id="tasktable"><tbody>` + strings.Replace(row, tt.remove, "", 1) + `</tbody></table>`)
			items, err := ParseAssignedTasks(body, assignedTasksPageURL)
			if err == nil {
				t.Fatalf("items = %#v, want an error for missing %s", items, tt.name)
			}
			if items != nil {
				t.Fatalf("items = %#v, want nil for missing %s", items, tt.name)
			}
		})
	}
}

func TestParseAssignedTasksRejectsDuplicateIDs(t *testing.T) {
	body := []byte(`<table id="tasktable"><tbody>
<tr data-id="501"><td class="c-name"><a href="/biz/task-view-501.html">重复任务一</a></td><td class="c-status">wait</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>
<tr data-id="501"><td class="c-name"><a href="/biz/task-view-501.html">重复任务二</a></td><td class="c-status">doing</td><td class="c-project">示例项目</td><td class="c-lastEditedDate">2026-11-27</td></tr>
</tbody></table>`)
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if err == nil {
		t.Fatalf("items = %#v, want an error for duplicate IDs", items)
	}
	if items != nil {
		t.Fatalf("items = %#v, want nil on duplicate IDs", items)
	}
	if !errors.Is(err, ErrDuplicateItemID) {
		t.Fatalf("error = %v, want ErrDuplicateItemID", err)
	}
}

func TestParseAssignedEmptyRecognizedTables(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		url   string
		parse func([]byte, string) ([]domain.Item, error)
	}{
		{
			name:  "modern tasks",
			body:  `<table id="tasktable"><tbody></tbody></table>`,
			url:   assignedTasksPageURL,
			parse: ParseAssignedTasks,
		},
		{
			name:  "modern bugs",
			body:  `<table id="bugList"></table>`,
			url:   assignedBugsPageURL,
			parse: ParseAssignedBugs,
		},
		{
			name:  "legacy tasks",
			body:  `<table><thead><tr><th>ID</th><th>Name</th><th>Status</th></tr></thead><tbody></tbody></table>`,
			url:   assignedTasksPageURL,
			parse: ParseAssignedTasks,
		},
		{
			name:  "legacy bugs",
			body:  `<table><thead><tr><th>Bug ID</th><th>Title</th><th>Status</th></tr></thead><tbody></tbody></table>`,
			url:   assignedBugsPageURL,
			parse: ParseAssignedBugs,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := tt.parse([]byte(tt.body), tt.url)
			if err != nil {
				t.Fatalf("parse empty table: %v", err)
			}
			if items == nil || len(items) != 0 {
				t.Fatalf("items = %#v, want a non-nil empty slice", items)
			}
		})
	}
}

func TestParseAssignedTasksRejectsLoginPage(t *testing.T) {
	body := readAssignedFixture(t, "login.html")
	items, err := ParseAssignedTasks(body, assignedTasksPageURL)
	if items != nil {
		t.Fatalf("items = %#v, want nil for a login page", items)
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
}

func readAssignedFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}
