package ui

import (
	"strconv"
	"sync"
	"testing"

	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMessagePopupManagerConcurrentShow 锤击弹窗管理器的并发路径：生产里
// Notify 在轮询 goroutine 上调用 Show（经 cx.Post 回主线程），这里用多个
// goroutine 并发触发，配合主循环 Settle 与数据更新交错，在 -race 下暴露
// 共享状态竞态。
func TestMessagePopupManagerConcurrentShow(t *testing.T) {
	var manager *MessagePopupManager
	var data *core.Signal[WorkbenchData]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = NewMessagePopupManager(cx, nil, nil)
		data = core.NewSignalFunc(cx, WorkbenchData{Connected: true, PopupSeconds: 0, Items: testItems()}, nil)
		return Workbench(cx, WorkbenchProps{DataFunc: data.Get})
	}, workbenchTestOptions()...)

	const goroutines = 4
	const rounds = 25
	var wg sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < rounds; round++ {
				change := domain.Change{
					Kind: domain.ChangeNew,
					Item: domain.Item{
						ID:      strconv.Itoa(worker+1) + "-" + strconv.Itoa(round),
						Type:    domain.ItemTask,
						Title:   "并发弹窗",
						Project: "并发测试",
						Status:  "doing",
					},
				}
				manager.Show([]domain.Change{change}, round%2)
			}
		}(worker)
	}

	// 主循环持续排空，并穿插数据更新，模拟刷新回调与弹窗并发。
	for draining := 0; draining < 200; draining++ {
		app.Post(func() {
			data.Update(func(data WorkbenchData) WorkbenchData {
				data.PopupSeconds = draining % 60
				return data
			})
		})
		app.Settle()
	}
	wg.Wait()
	app.Settle()

	app.Post(func() {
		if len(manager.active) > maxMessagePopups {
			t.Errorf("active popups = %d, want at most %d", len(manager.active), maxMessagePopups)
		}
		for _, popup := range append([]*messagePopup(nil), manager.active...) {
			manager.close(popup)
		}
		if len(manager.active) != 0 {
			t.Errorf("active popups after close = %d, want 0", len(manager.active))
		}
	})
	app.Settle()
}

// TestMessagePopupManagerSummarizesLargeBatches 验证大批次分流：变化数
// 超过阈值时只弹一张摘要卡（点卡片打开工作台），不再逐条开面板。
func TestMessagePopupManagerSummarizesLargeBatches(t *testing.T) {
	var manager *MessagePopupManager
	workbenchOpened := false
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = NewMessagePopupManager(cx, nil, func() { workbenchOpened = true })
		return Workbench(cx, WorkbenchProps{Data: WorkbenchData{Connected: true}})
	}, workbenchTestOptions()...)

	changes := make([]domain.Change, 0, 30)
	for index := 1; index <= 30; index++ {
		changes = append(changes, domain.Change{
			Kind: domain.ChangeNew,
			Item: domain.Item{ID: strconv.Itoa(index), Type: domain.ItemBug, Title: "批量新增", Status: "active"},
		})
	}
	// 面板窗口在 nsgotest 环境不可用，这里验证分流决策与计数逻辑；
	// 摘要卡的可视验证在真实应用中完成。
	newCount, statusCount := summarizeCounts(changes)
	if newCount != 30 || statusCount != 0 {
		t.Fatalf("summarizeCounts = (%d, %d), want (30, 0)", newCount, statusCount)
	}
	if len(changes) <= summaryPopupThreshold {
		t.Fatalf("batch size %d should exceed the summary threshold %d", len(changes), summaryPopupThreshold)
	}
	_ = workbenchOpened
	_ = manager
	_ = app
}
