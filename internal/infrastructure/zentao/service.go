// Package zentao implements the authenticated, read-only Zentao gateway.
package zentao

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
)

const (
	serviceRecPerPage = 100
	maxServicePages   = 100
)

var (
	ErrConflictingDuplicate = errors.New("conflicting duplicate zentao item")
	ErrPageLimitExceeded    = errors.New("zentao pagination exceeded 100 pages")
)

// Service aggregates all assigned task and bug pages into one snapshot.
type Service struct {
	client *Client
}

// NewService creates a Zentao gateway backed by client.
func NewService(client *Client) *Service {
	return &Service{client: client}
}

// Login delegates session establishment to the HTTP client.
func (s *Service) Login(ctx context.Context) error {
	return s.client.Login(ctx)
}

// FetchAssignedItems returns a complete, deduplicated snapshot sorted by item key.
// Any failure discards all items accumulated during the refresh.
func (s *Service) FetchAssignedItems(ctx context.Context) ([]domain.Item, error) {
	tasks, err := s.fetchPages(ctx, "task", s.client.FetchAssignedTasksPage, ParseAssignedTasks)
	if err != nil {
		return nil, err
	}
	bugs, err := s.fetchPages(ctx, "bug", s.client.FetchAssignedBugsPage, ParseAssignedBugs)
	if err != nil {
		return nil, err
	}

	itemsByKey := make(map[string]domain.Item, len(tasks)+len(bugs))
	for _, items := range [][]domain.Item{tasks, bugs} {
		for _, item := range items {
			key := item.Key()
			if previous, exists := itemsByKey[key]; exists && previous != item {
				return nil, fmt.Errorf("%w: %s", ErrConflictingDuplicate, key)
			}
			itemsByKey[key] = item
		}
	}
	items := make([]domain.Item, 0, len(itemsByKey))
	for _, item := range itemsByKey {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key() < items[j].Key() })
	return items, nil
}

type assignedPageParser func([]byte, string) ([]domain.Item, error)

func (s *Service) fetchPages(
	ctx context.Context,
	objectType string,
	fetch func(context.Context, int, int) ([]byte, error),
	parse assignedPageParser,
) ([]domain.Item, error) {
	var all []domain.Item
	for pageID := 1; pageID <= maxServicePages; pageID++ {
		body, err := fetch(ctx, pageID, serviceRecPerPage)
		if err != nil {
			return nil, fmt.Errorf("fetch assigned %s page %d: %w", objectType, pageID, serviceError(err))
		}
		pageURL := s.client.BaseURL + assignedPagePath(objectType, pageID, serviceRecPerPage)
		items, err := parse(body, pageURL)
		if err != nil && !errors.Is(err, ErrPaginationRequired) {
			return nil, fmt.Errorf("parse assigned %s page %d: %w", objectType, pageID, serviceError(err))
		}
		all = append(all, items...)
		if err == nil {
			return all, nil
		}
		if pageID == maxServicePages {
			return nil, fmt.Errorf("%w: assigned %s", ErrPageLimitExceeded, objectType)
		}
	}
	return nil, fmt.Errorf("%w: assigned %s", ErrPageLimitExceeded, objectType)
}

func serviceError(err error) error {
	if errors.Is(err, ErrSessionExpired) {
		return application.ErrSessionExpired
	}
	return err
}

var _ application.ZentaoGateway = (*Service)(nil)
