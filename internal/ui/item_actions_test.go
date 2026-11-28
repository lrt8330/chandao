package ui

import (
	"testing"

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
