// Package application defines use-case ports independent of their implementations.
package application

import "context"

// ZentaoClient provides authenticated access to the current user's Zentao pages.
type ZentaoClient interface {
	Login(ctx context.Context) error
	FetchAssignedTasks(ctx context.Context) ([]byte, error)
	FetchAssignedBugs(ctx context.Context) ([]byte, error)
}
