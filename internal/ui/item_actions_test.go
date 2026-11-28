package ui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"chandao/zentaotray/internal/domain"
)

// 操作流：选中进行中的任务 → 详情出现操作按钮 → 按完成 → 确认弹窗 →
// 确认后回调收到正确的对象与动作；错误信息回显到操作行。
func TestItemActionFlowFromDetailToConfirm(t *testing.T) {
	var (
		actionItem domain.Item
		actionID   string
		invokeDone func(string)
	)
	props := WorkbenchProps{Data: WorkbenchData{Connected: true, Items: []domain.Item{
		{ID: "108", Type: domain.ItemTask, Title: "并发压测-任务", Project: "示例项目", Status: "doing", UpdatedAt: "2026-11-27 10:30:00", URL: "https://zentao.example.invalid/biz/task-view-108.html"},
	}}}
	props.OnRunItemAction = func(item domain.Item, action string, done func(string)) {
		actionItem, actionID, invokeDone = item, action, done
	}
	app := startWorkbench(t, props)

	app.Press("id:item-list >> text:并发压测-任务")
	app.Settle()
	app.Find("id:item-detail")
	// doing 任务的操作是 完成/暂停/取消。
	app.Find("id:item-action-finish")
	app.Find("id:item-action-pause")
	app.Find("id:item-action-cancel")

	app.Press("id:item-action-finish")
	app.Settle()
	app.Find("id:item-action-confirm")
	app.Find(`id:item-action-confirm >> text:确认完成？`)

	app.Press("id:confirm-item-action")
	app.Settle()
	if actionID != "finish" || actionItem.ID != "108" || actionItem.Type != domain.ItemTask {
		t.Fatalf("OnRunItemAction item=%#v action=%q", actionItem, actionID)
	}
	if invokeDone == nil {
		t.Fatal("done callback missing")
	}
	invokeDone("网络连接异常或禅道暂时不可用，请稍后重试。")
	app.Settle()
	if !app.Exists(`id:item-actions >> text:网络连接异常或禅道暂时不可用，请稍后重试。`) {
		t.Fatal("action error feedback is not shown")
	}
}

// 取消路径：打开确认弹窗后点取消，不触发任何回调。
func TestItemActionCancelDoesNotInvoke(t *testing.T) {
	called := false
	props := WorkbenchProps{Data: WorkbenchData{Connected: true, Items: []domain.Item{
		{ID: "109", Type: domain.ItemBug, Title: "登录页样式错乱", Project: "示例项目", Status: "active", UpdatedAt: "2026-11-27 11:00:00", URL: "https://zentao.example.invalid/biz/bug-view-109.html"},
	}}}
	props.OnRunItemAction = func(item domain.Item, action string, done func(string)) {
		called = true
	}
	app := startWorkbench(t, props)

	app.Press("id:item-list >> text:登录页样式错乱")
	app.Settle()
	app.Find("id:item-action-resolve")
	app.Find("id:item-action-close")

	app.Press("id:item-action-resolve")
	app.Settle()
	app.Find("id:item-action-confirm")
	app.Press("id:cancel-item-action")
	app.Settle()
	app.WaitForGone("id:item-action-confirm", 1<<10)
	if called {
		t.Fatal("cancel must not invoke the action callback")
	}
}

// 确认框打开后状态被刷新改变：关闭确认框且不提交过期操作。
func TestConfirmRejectsStatusChangedWhileOpen(t *testing.T) {
	items := []domain.Item{
		{ID: "108", Type: domain.ItemTask, Title: "并发压测-任务", Project: "示例项目", Status: "doing", URL: "https://zentao.example.invalid/biz/task-view-108.html"},
	}
	calls := 0
	var state *core.Signal[WorkbenchData]
	props := WorkbenchProps{
		DataFunc: func() WorkbenchData { return state.Get() },
		OnRunItemAction: func(domain.Item, string, func(string)) {
			calls++
		},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		state = core.NewSignalFunc(cx, WorkbenchData{Connected: true, Items: items}, nil)
		return Workbench(cx, props)
	}, workbenchTestOptions()...)

	app.Press("id:item-list >> text:并发压测-任务")
	app.Settle()
	app.Press("id:item-action-finish")
	app.Settle()
	app.Find("id:item-action-confirm")

	paused := append([]domain.Item(nil), items...)
	paused[0].Status = "pause"
	app.Post(func() { state.Set(WorkbenchData{Connected: true, Items: paused}) })
	app.Settle()
	app.WaitForGone("id:item-action-confirm", time.Second)
	if calls != 0 {
		t.Fatalf("stale action submitted %d times", calls)
	}
}

// 请求执行中再次点击同一操作的确认：不得重复提交。
func TestItemActionSubmitsOnceWhileRunning(t *testing.T) {
	items := []domain.Item{
		{ID: "108", Type: domain.ItemTask, Title: "慢请求任务", Project: "示例项目", Status: "doing", URL: "https://zentao.example.invalid/biz/task-view-108.html"},
	}
	calls := 0
	var done func(string)
	props := WorkbenchProps{Data: WorkbenchData{Connected: true, Items: items}}
	props.OnRunItemAction = func(domain.Item, string, func(string)) { calls++; done = func(string) {} }
	app := startWorkbench(t, props)

	app.Press("id:item-list >> text:慢请求任务")
	app.Settle()
	app.Press("id:item-action-finish")
	app.Settle()
	app.Press("id:confirm-item-action")
	app.Settle()
	if calls != 1 {
		t.Fatalf("action calls = %d, want 1", calls)
	}
	// 执行中按钮应禁用，重复点击不会再次弹确认框或提交。
	if !app.Find("id:item-action-finish").Props.Disabled {
		t.Fatal("action button is not disabled while the request is running")
	}
	if app.Exists("id:item-action-confirm") {
		t.Fatal("confirm dialog reopened while a request was running")
	}
	if calls != 1 {
		t.Fatalf("action calls = %d, want 1", calls)
	}
	if done == nil {
		t.Fatal("done callback missing")
	}
}
