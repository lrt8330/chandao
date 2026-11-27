// Package notification adapts application changes to NSGo notifications.
package notification

import (
	"context"
	"fmt"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
	"github.com/nsgo-dev/nsgo/core"
)

const maxDetailNotifications = 5

// BuildNotifications converts changes into stable, credential-free notifications.
func BuildNotifications(changes []domain.Change) []core.Notification {
	if len(changes) == 0 {
		return nil
	}

	detailCount := len(changes)
	if detailCount > maxDetailNotifications {
		detailCount = maxDetailNotifications
	}
	notifications := make([]core.Notification, 0, detailCount+1)
	for _, change := range changes[:detailCount] {
		body := fmt.Sprintf("标题：%s\n项目：%s\n当前状态：%s", change.Item.Title, change.Item.Project, change.Item.Status)
		if change.Kind == domain.ChangeStatus {
			body += fmt.Sprintf("\n状态变化：%s→%s", change.Old, change.New)
		}
		notifications = append(notifications, core.Notification{
			ID:    change.Item.Key(),
			Title: notificationTitle(change),
			Body:  body,
		})
	}
	if len(changes) > maxDetailNotifications {
		notifications = append(notifications, core.Notification{
			ID:    "summary",
			Title: "禅道事项更新",
			Body:  fmt.Sprintf("本轮共有 %d 条新增或状态变化", len(changes)),
		})
	}
	return notifications
}

func notificationTitle(change domain.Change) string {
	switch change.Kind {
	case domain.ChangeNew:
		if change.Item.Type == domain.ItemBug {
			return "新增缺陷"
		}
		return "新增任务"
	case domain.ChangeStatus:
		if change.Item.Type == domain.ItemBug {
			return "缺陷状态变更"
		}
		return "任务状态变更"
	default:
		return "禅道事项更新"
	}
}

// WindowsNotifier delivers notifications through an NSGo scope.
type WindowsNotifier struct {
	Cx *core.Scope
}

// Notify posts notification delivery without surfacing errors to the refresh flow.
func (n WindowsNotifier) Notify(ctx context.Context, changes []domain.Change) {
	if n.Cx == nil {
		return
	}
	notifications := BuildNotifications(changes)
	if len(notifications) == 0 {
		return
	}
	n.Cx.Post(func() {
		for _, notification := range notifications {
			_ = n.Cx.ShowNotification(notification, nil)
		}
	})
}

var _ application.Notifier = WindowsNotifier{}
