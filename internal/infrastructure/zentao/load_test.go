package zentao

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
)

// loadTestItem 描述一条压测指派事项。
type loadTestItem struct {
	ID      string
	Kind    string // task / bug
	Title   string
	Status  string
	Project string
	Edited  string // lastEditedDate 单元格内容
}

// loadTestStore 是并发安全的内存快照仓。
type loadTestStore struct {
	mu       sync.Mutex
	snapshot application.Snapshot
	saves    int
}

func (s *loadTestStore) Load(ctx context.Context) (application.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneLoadSnapshot(s.snapshot), nil
}

func (s *loadTestStore) Save(ctx context.Context, snapshot application.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = cloneLoadSnapshot(snapshot)
	s.saves++
	return nil
}

func (s *loadTestStore) current() application.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneLoadSnapshot(s.snapshot)
}

func cloneLoadSnapshot(snapshot application.Snapshot) application.Snapshot {
	clone := snapshot
	clone.Items = make(map[string]domain.Item, len(snapshot.Items))
	for key, item := range snapshot.Items {
		clone.Items[key] = item
	}
	return clone
}

// loadTestNotifier 收集全部通知批次。
type loadTestNotifier struct {
	mu      sync.Mutex
	batches [][]domain.Change
}

func (n *loadTestNotifier) Notify(ctx context.Context, changes []domain.Change) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.batches = append(n.batches, append([]domain.Change(nil), changes...))
}

func (n *loadTestNotifier) total() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	total := 0
	for _, batch := range n.batches {
		total += len(batch)
	}
	return total
}

func (n *loadTestNotifier) batchCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.batches)
}

// loadTestBuildItems 生成 count/2 个任务 + count/2 个 Bug，分属 10 个项目。
func loadTestBuildItems(count int) []loadTestItem {
	projects := []string{"云平台", "移动端", "开放平台", "数据中台", "桌面端"}
	taskStatuses := []string{"doing", "wait", "pause"}
	// Bug 只有 active 一个未完成状态；已完成（closed/resolved）会被
	// 服务端过滤，不进入指派列表。
	bugStatuses := []string{"active"}
	items := make([]loadTestItem, 0, count)
	half := count / 2
	for index := 1; index <= half; index++ {
		items = append(items, loadTestItem{
			ID:      fmt.Sprint(index),
			Kind:    "task",
			Title:   fmt.Sprintf("压测任务-%03d", index),
			Status:  taskStatuses[index%len(taskStatuses)],
			Project: projects[index%len(projects)],
			Edited:  "2026-11-27 08:00:00",
		})
	}
	for index := 1; index <= count-half; index++ {
		items = append(items, loadTestItem{
			ID:      fmt.Sprint(index),
			Kind:    "bug",
			Title:   fmt.Sprintf("压测缺陷-%03d", index),
			Status:  bugStatuses[index%len(bugStatuses)],
			Project: projects[index%len(projects)],
			Edited:  "2026-11-27 08:00:00",
		})
	}
	return items
}

// loadTestPage 渲染一页指派表格。
func loadTestPage(kind string, items []loadTestItem, total int) string {
	tableID := "tasktable"
	if kind == "bug" {
		tableID = "bugList"
	}
	var body strings.Builder
	for _, item := range items {
		fmt.Fprintf(&body, `<tr data-id="%s"><td class="c-name"><a href="/biz/%s-view-%s.html">%s</a></td><td class="c-status">%s</td><td class="c-project">%s</td><td class="c-lastEditedDate">%s</td></tr>`,
			item.ID, kind, item.ID, item.Title, item.Status, item.Project, item.Edited)
	}
	return fmt.Sprintf(`<input id="recTotal" value="%d"><input id="recPerPage" value="100"><table id="%s"><tbody>%s</tbody></table>`, total, tableID, body.String())
}

// loadTestFlip 在任务的三个未完成状态间轮换，保证旧新必不相同；
// Bug 的唯一未完成状态是 active，翻转保持不变。
func loadTestFlip(kind, status string, flipped bool) string {
	if !flipped || kind != "task" {
		return status
	}
	switch status {
	case "doing":
		return "wait"
	case "wait":
		return "pause"
	case "pause":
		return "doing"
	}
	return status
}

// TestThousandAssignedItemsConcurrentRefresh 用模拟禅道承载 1000 条指派
// （500 任务 + 500 Bug，10 个项目），对 Controller.Refresh 全链路做并发
// 压测：基线首扫、并发刷新风暴、全员状态翻转的变化风暴。在 -race 下运行。
func TestThousandAssignedItemsConcurrentRefresh(t *testing.T) {
	const total = 1000
	const perPage = 100
	all := loadTestBuildItems(total)
	tasks := make([]loadTestItem, 0, total/2)
	bugs := make([]loadTestItem, 0, total/2)
	statuses := make(map[string]string, total)
	for _, item := range all {
		statuses[item.Kind+":"+item.ID] = item.Status
		if item.Kind == "task" {
			tasks = append(tasks, item)
		} else {
			bugs = append(bugs, item)
		}
	}

	// flipped 翻转全部状态，制造 1000 条变化的风暴。
	var flipped atomic.Bool
	var loginCalls, pageFetches atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		switch {
		case query.Get("m") == "user" && query.Get("f") == "refreshRandom":
			fmt.Fprint(w, "424242")
		case r.URL.Path == "/biz/index.php" && query.Get("m") == "user" && query.Get("f") == "login":
			loginCalls.Add(1)
			fmt.Fprint(w, `{"result":"success","locate":"/biz/index.php?m=my&f=index"}`)
		case r.URL.Path == "/biz/index.php" && query.Get("m") == "my" && query.Get("f") == "task":
			pageFetches.Add(1)
			ordered := append([]loadTestItem(nil), tasks...)
			if flipped.Load() {
				for index := range ordered {
					ordered[index].Status = loadTestFlip("task", statuses["task:"+ordered[index].ID], true)
					ordered[index].Edited = time.Now().Format("2006-01-02 15:04:05")
				}
				sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Edited > ordered[j].Edited })
			}
			page, _ := strconv.Atoi(query.Get("pageID"))
			start := (page - 1) * perPage
			end := start + perPage
			if end > len(ordered) {
				end = len(ordered)
			}
			slice := append([]loadTestItem(nil), ordered[start:end]...)
			fmt.Fprint(w, loadTestPage("task", slice, len(ordered)))
		case r.URL.Path == "/biz/index.php" && query.Get("m") == "my" && query.Get("f") == "bug":
			pageFetches.Add(1)
			ordered := append([]loadTestItem(nil), bugs...)
			if flipped.Load() {
				for index := range ordered {
					ordered[index].Edited = time.Now().Format("2006-01-02 15:04:05")
				}
				sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Edited > ordered[j].Edited })
			}
			page, _ := strconv.Atoi(query.Get("pageID"))
			start := (page - 1) * perPage
			end := start + perPage
			if end > len(ordered) {
				end = len(ordered)
			}
			slice := append([]loadTestItem(nil), ordered[start:end]...)
			fmt.Fprint(w, loadTestPage("bug", slice, len(ordered)))
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL+"/biz/", "admin", "load-test-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	store := &loadTestStore{}
	store.snapshot = application.Snapshot{
		Items:                map[string]domain.Item{},
		PollSeconds:          60,
		NotificationsEnabled: true,
		PopupSeconds:         8,
	}
	notifier := &loadTestNotifier{}
	controller, err := application.NewController(context.Background(), NewService(client), store, notifier)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	// 基线首扫：1000 条入库，首扫不通知。
	began := time.Now()
	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("baseline refresh: %v", err)
	}
	baselineDuration := time.Since(began)
	if len(result.Snapshot.Items) != total {
		t.Fatalf("baseline items = %d, want %d", len(result.Snapshot.Items), total)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("baseline changes = %d, want 0", len(result.Changes))
	}
	if got := notifier.total(); got != 0 {
		t.Fatalf("baseline notifications = %d, want 0", got)
	}

	// 并发风暴：8 goroutine × 5 次 Refresh 全链路。串行化后恰好一次刷新
	// 承载全部变化（未翻转时为 0，翻转后为任务总数），其余刷新为 0。
	storm := func(wantTotalChanges int) []time.Duration {
		var durations []time.Duration
		var mu sync.Mutex
		var wg sync.WaitGroup
		seenChanges := 0
		const workers, rounds = 8, 5
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for round := 0; round < rounds; round++ {
					begin := time.Now()
					result, err := controller.Refresh(context.Background())
					spent := time.Since(begin)
					mu.Lock()
					durations = append(durations, spent)
					seenChanges += len(result.Changes)
					mu.Unlock()
					if err != nil {
						t.Errorf("storm refresh: %v", err)
						return
					}
					if len(result.Snapshot.Items) != total {
						t.Errorf("storm items = %d, want %d", len(result.Snapshot.Items), total)
					}
					if got := len(result.Changes); got != 0 && got != wantTotalChanges {
						t.Errorf("storm changes = %d, want 0 or %d", got, wantTotalChanges)
					}
				}
			}()
		}
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		if seenChanges != wantTotalChanges {
			t.Errorf("storm total changes = %d, want %d", seenChanges, wantTotalChanges)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		return durations
	}

	storm1 := storm(0)
	if got := notifier.total(); got != 0 {
		t.Fatalf("storm1 notifications = %d, want 0", got)
	}

	// 翻转全部任务状态后并发风暴二：恰好一次刷新携带 500 条任务状态变化
	//（Bug 只有 active 一个未完成状态，翻转不产生变化）。
	const expectedChanges = total / 2
	flipped.Store(true)
	storm2 := storm(expectedChanges)
	if got := notifier.batchCount(); got != 1 {
		t.Fatalf("storm2 notification batches = %d, want 1", got)
	}
	if got := notifier.total(); got != expectedChanges {
		t.Fatalf("storm2 notified changes = %d, want %d", got, expectedChanges)
	}
	notifier.mu.Lock()
	for _, change := range notifier.batches[0] {
		if change.Kind != domain.ChangeStatus {
			notifier.mu.Unlock()
			t.Fatalf("change kind = %v, want status", change.Kind)
		}
	}
	notifier.mu.Unlock()

	t.Logf("baseline refresh: %s", baselineDuration.Round(time.Millisecond))
	t.Logf("storm1 p50/p95/max: %s / %s / %s",
		storm1[len(storm1)/2].Round(time.Millisecond),
		storm1[len(storm1)*95/100].Round(time.Millisecond),
		storm1[len(storm1)-1].Round(time.Millisecond))
	t.Logf("storm2 p50/p95/max: %s / %s / %s",
		storm2[len(storm2)/2].Round(time.Millisecond),
		storm2[len(storm2)*95/100].Round(time.Millisecond),
		storm2[len(storm2)-1].Round(time.Millisecond))
	t.Logf("page fetches: %d, logins: %d, store saves: %d", pageFetches.Load(), loginCalls.Load(), store.saves)
}
