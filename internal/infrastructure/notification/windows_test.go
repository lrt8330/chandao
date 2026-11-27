package notification

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"chandao/zentaotray/internal/domain"
)

func TestBuildNotificationsOmitsCredentialsAndLabelsBugStatus(t *testing.T) {
	change := domain.Change{
		Kind: domain.ChangeStatus,
		Item: domain.Item{
			ID:        "201",
			Type:      domain.ItemBug,
			Title:     "缺陷状态变化",
			Project:   "订单系统",
			Status:    "resolved",
			UpdatedAt: "2026-11-27",
			URL:       "https://alice:password@example.invalid/biz/bug-view-201.html?token=secret-token",
		},
		Old: "active",
		New: "resolved",
	}
	got := BuildNotifications([]domain.Change{change})
	if len(got) != 1 {
		t.Fatalf("notifications = %#v, want one", got)
	}
	if got[0].Title != "缺陷状态变更" || !strings.Contains(got[0].Body, "active→resolved") {
		t.Errorf("notification = %#v, want a bug status change", got[0])
	}
	for _, sensitive := range []string{"alice", "password", "secret-token", "example.invalid", change.Item.URL} {
		if strings.Contains(got[0].Body, sensitive) || strings.Contains(got[0].ID, sensitive) || strings.Contains(got[0].Title, sensitive) {
			t.Errorf("notification contains credential or URL data %q: %#v", sensitive, got[0])
		}
	}
}

func TestBuildNotificationsLimitsDetailsAndAddsSummary(t *testing.T) {
	for _, count := range []int{0, 1, 5, 6, 9} {
		t.Run(fmt.Sprintf("changes_%d", count), func(t *testing.T) {
			changes := make([]domain.Change, count)
			for i := range changes {
				itemType := domain.ItemTask
				if i%2 == 1 {
					itemType = domain.ItemBug
				}
				changes[i] = domain.Change{
					Kind: domain.ChangeNew,
					Item: domain.Item{ID: strconv.Itoa(100 + i), Type: itemType, Title: "事项", Project: "项目", Status: "wait"},
				}
			}
			got := BuildNotifications(changes)
			if again := BuildNotifications(changes); !reflect.DeepEqual(got, again) {
				t.Fatalf("repeated build changed notifications: %#v vs %#v", got, again)
			}
			wantCount := count
			if wantCount > 5 {
				wantCount = 6
			}
			if len(got) != wantCount {
				t.Fatalf("notifications = %#v, want %d", got, wantCount)
			}
			for i := 0; i < count && i < 5; i++ {
				if got[i].ID != changes[i].Item.Key() {
					t.Errorf("notification[%d].ID = %q, want stable ID %q", i, got[i].ID, changes[i].Item.Key())
				}
			}
			if count > 5 {
				summary := got[5]
				if summary.ID != "summary" || summary.Title != "禅道事项更新" || summary.Body != fmt.Sprintf("本轮共有 %d 条新增或状态变化", count) {
					t.Errorf("summary = %#v, want the total change count", summary)
				}
			}
		})
	}
}

func TestBuildNotificationsFormatsChineseDetails(t *testing.T) {
	changes := []domain.Change{
		{
			Kind: domain.ChangeNew,
			Item: domain.Item{ID: "101", Type: domain.ItemTask, Title: "修复登录问题", Project: "用户平台", Status: "wait"},
		},
		{
			Kind: domain.ChangeNew,
			Item: domain.Item{ID: "201", Type: domain.ItemBug, Title: "修复重复提示", Project: "订单系统", Status: "active"},
		},
		{
			Kind: domain.ChangeStatus,
			Item: domain.Item{ID: "102", Type: domain.ItemTask, Title: "整理测试记录", Project: "质量平台", Status: "doing"},
			Old:  "wait",
			New:  "doing",
		},
	}

	got := BuildNotifications(changes)
	if len(got) != 3 {
		t.Fatalf("notifications = %#v, want 3 details", got)
	}
	wantIDs := []string{"task:101", "bug:201", "task:102"}
	wantTitles := []string{"新增任务", "新增缺陷", "任务状态变更"}
	for i, notification := range got {
		if notification.ID != wantIDs[i] {
			t.Errorf("notification[%d].ID = %q, want %q", i, notification.ID, wantIDs[i])
		}
		if notification.Title != wantTitles[i] {
			t.Errorf("notification[%d].Title = %q, want %q", i, notification.Title, wantTitles[i])
		}
		for _, text := range []string{changes[i].Item.Title, changes[i].Item.Project, changes[i].Item.Status} {
			if !strings.Contains(notification.Body, text) {
				t.Errorf("notification[%d].Body = %q, want %q", i, notification.Body, text)
			}
		}
	}
	if !strings.Contains(got[2].Body, "wait→doing") {
		t.Errorf("status change body = %q, want old→new statuses", got[2].Body)
	}
}
