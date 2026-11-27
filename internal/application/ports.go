// Package application defines use-case ports independent of their implementations.
package application

import (
	"context"

	"chandao/zentaotray/internal/domain"
)

// ZentaoClient provides authenticated access to the current user's Zentao pages.
type ZentaoClient interface {
	Login(ctx context.Context) error
	FetchAssignedTasks(ctx context.Context) ([]byte, error)
	FetchAssignedBugs(ctx context.Context) ([]byte, error)
}

// ZentaoGateway provides authenticated access to the current user's assigned items.
type ZentaoGateway interface {
	Login(ctx context.Context) error
	FetchAssignedItems(ctx context.Context) ([]domain.Item, error)
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
