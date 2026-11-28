// Package zentao implements the authenticated Zentao gateway: assigned-item
// sync (read) and item lifecycle actions (write).
package zentao

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
)

const (
	serviceRecPerPage = 100
	// maxServicePages 支撑超大账号（如 10 万条指派 = 1000 页）并保留
	// 防失控上限。
	maxServicePages = 2000
	// incrementalPageBudget 限制单次增量同步的页数：正常增量只有几页；
	// 超预算说明变化面过大，调用方应回退全量对账。
	incrementalPageBudget = 50
)

var (
	ErrConflictingDuplicate      = errors.New("conflicting duplicate zentao item")
	ErrPageLimitExceeded         = errors.New("zentao pagination exceeded page budget")
	ErrIncrementalBudgetExceeded = errors.New("incremental sync exceeded page budget")
)

// Service aggregates all assigned task and bug pages into one snapshot.
type Service struct {
	client *Client
}

// NewService creates a Zentao gateway backed by client.
func NewService(client *Client) *Service {
	return &Service{client: client}
}

// RunItemAction 以当前会话执行一条事项操作（开始/完成/解决/关闭等）：
// 抓取操作页表单、回填隐藏字段与预填值后原样提交。
func (s *Service) RunItemAction(ctx context.Context, itemType domain.ItemType, id, action string) error {
	form, err := s.client.FetchActionForm(ctx, string(itemType), action, id)
	if err != nil {
		return err
	}
	if _, err := s.client.SubmitAction(ctx, form, nil); err != nil {
		return err
	}
	return nil
}

// Login delegates session establishment to the HTTP client.
func (s *Service) Login(ctx context.Context) error {
	return s.client.Login(ctx)
}

// FetchAssignedItemsUpdatedSince 增量抓取 lastEditedDate 晚于 since 的指派
// 事项（含已完成状态，调用方据此从本地索引移除终结事项）。按
// lastEditedDate 倒序翻页，出现整页均不晚于 since、或不足一页、或超出
// 页预算时停止；超预算返回 ErrIncrementalBudgetExceeded。需求抓取失败
// 仅降级（返回任务和 Bug），不阻断整轮刷新。
func (s *Service) FetchAssignedItemsUpdatedSince(ctx context.Context, since time.Time) ([]domain.Item, error) {
	tasks, err := s.fetchPagesSince(ctx, "task", since)
	if err != nil {
		return nil, err
	}
	bugs, err := s.fetchPagesSince(ctx, "bug", since)
	if err != nil {
		return nil, err
	}
	// 需求页与任务/Bug 的列结构存在差异，解析失败时不阻断同步。
	stories, storyErr := s.fetchPagesSince(ctx, "story", since)
	if storyErr != nil {
		stories = nil
	}
	itemsByKey := make(map[string]domain.Item, len(tasks)+len(bugs)+len(stories))
	for _, items := range [][]domain.Item{tasks, bugs, stories} {
		for _, item := range items {
			itemsByKey[item.Key()] = item
		}
	}
	items := make([]domain.Item, 0, len(itemsByKey))
	for _, item := range itemsByKey {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key() < items[j].Key() })
	return items, nil
}

func (s *Service) fetchPagesSince(ctx context.Context, objectType string, since time.Time) ([]domain.Item, error) {
	var all []domain.Item
	for pageID := 1; pageID <= incrementalPageBudget; pageID++ {
		body, err := s.client.FetchAssignedPage(ctx, objectType, pageID, serviceRecPerPage, "lastEditedDate_desc")
		if err != nil {
			return nil, fmt.Errorf("fetch updated %s page %d: %w", objectType, pageID, serviceError(err))
		}
		pageURL := s.client.BaseURL + assignedPagePathOrdered(objectType, "lastEditedDate_desc", pageID, serviceRecPerPage)
		items, err := parseUpdatedPage(body, pageURL, objectType)
		if err != nil && !errors.Is(err, ErrPaginationRequired) {
			return nil, fmt.Errorf("parse updated %s page %d: %w", objectType, pageID, serviceError(err))
		}
		all = append(all, items...)
		if len(items) < serviceRecPerPage {
			return all, nil
		}
		if oldest := oldestLastEdited(items); !oldest.After(since) {
			return all, nil
		}
	}
	return nil, ErrIncrementalBudgetExceeded
}

// parseUpdatedPage 解析一页含全部状态的事项行。
func parseUpdatedPage(body []byte, pageURL string, objectType string) ([]domain.Item, error) {
	switch objectType {
	case "bug":
		return ParseAssignedBugsWithFinished(body, pageURL)
	case "story":
		return ParseAssignedStoriesWithFinished(body, pageURL)
	}
	return ParseAssignedTasksWithFinished(body, pageURL)
}

// oldestLastEdited 返回一页中最早的 lastEditedDate；无法解析的条目视为
// 仍然新鲜（保守地继续翻页）。
func oldestLastEdited(items []domain.Item) time.Time {
	oldest := time.Now().Add(time.Hour)
	found := false
	for _, item := range items {
		if parsed, ok := parseLastEdited(item.UpdatedAt); ok {
			if !found || parsed.Before(oldest) {
				oldest, found = parsed, true
			}
		}
	}
	if !found {
		return time.Now().Add(time.Hour)
	}
	return oldest
}

func parseLastEdited(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// FetchAssignedItems returns a complete, deduplicated snapshot sorted by item key.
// Any failure discards all items accumulated during the refresh. 需求抓取
// 失败仅降级（快照缺需求），不阻断任务和 Bug 的刷新。
func (s *Service) FetchAssignedItems(ctx context.Context) ([]domain.Item, error) {
	tasks, err := s.fetchPages(ctx, "task", s.client.FetchAssignedTasksPage, ParseAssignedTasks)
	if err != nil {
		return nil, err
	}
	bugs, err := s.fetchPages(ctx, "bug", s.client.FetchAssignedBugsPage, ParseAssignedBugs)
	if err != nil {
		return nil, err
	}
	// 需求页与任务/Bug 的列结构存在差异，解析失败时不阻断全量刷新。
	stories, storyErr := s.fetchPages(ctx, "story", s.client.FetchAssignedStoriesPage, ParseAssignedStories)
	if storyErr != nil {
		stories = nil
	}

	itemsByKey := make(map[string]domain.Item, len(tasks)+len(bugs)+len(stories))
	for _, items := range [][]domain.Item{tasks, bugs, stories} {
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
	if errors.Is(err, ErrIncrementalBudgetExceeded) {
		return application.ErrIncrementalBudgetExceeded
	}
	return err
}

var _ application.ZentaoGateway = (*Service)(nil)
