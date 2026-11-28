// Package application defines use-case ports independent of their implementations.
package application

import (
	"context"
	"time"

	"chandao/zentaotray/internal/domain"
)

// ZentaoGateway provides authenticated access to the current user's assigned items.
type ZentaoGateway interface {
	Login(ctx context.Context) error
	FetchAssignedItems(ctx context.Context) ([]domain.Item, error)
	// FetchAssignedItemsUpdatedSince 增量抓取 lastEditedDate 晚于 since 的
	// 事项（含已完成状态）。超出预算时返回 ErrIncrementalBudgetExceeded，
	// 调用方应回退全量抓取。
	FetchAssignedItemsUpdatedSince(ctx context.Context, since time.Time) ([]domain.Item, error)
	// RunItemAction 对一条事项执行生命周期操作（task: start/finish/pause/
	// cancel/close；bug: resolve/activate/close）。会话失效返回会话过期类
	// 错误，调用方可重新登录后重试。
	RunItemAction(ctx context.Context, itemType domain.ItemType, id, action string) error
}

// Notifier delivers changes detected by a successful refresh.
type Notifier interface {
	Notify(ctx context.Context, changes []domain.Change)
}

// SnapshotStore loads and saves the local application state.
type SnapshotStore interface {
	Load(ctx context.Context) (Snapshot, error)
	Save(ctx context.Context, snapshot Snapshot) error
}

// CredentialStore keeps account values in a secure store.
type CredentialStore interface {
	Get(account string) (string, bool, error)
	Set(account, value string) error
	Delete(account string) error
}
