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
