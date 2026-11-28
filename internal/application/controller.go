package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"chandao/zentaotray/internal/domain"
)

var (
	ErrSessionExpired            = errors.New("session expired")
	ErrIncrementalBudgetExceeded = errors.New("incremental sync exceeded page budget")
	ErrStateLoad                 = errors.New("load application state")
	ErrInvalidSettings           = errors.New("invalid settings")
	ErrItemChanged               = errors.New("item no longer exists or its status changed")
)

// RefreshResult is the snapshot and changes produced by a successful refresh.
type RefreshResult struct {
	Snapshot Snapshot
	Changes  []domain.Change
}

// reconcileEvery 每隔多少次成功刷新做一次全量对账，纠正增量可能错过
// 的删除与漂移。
const (
	// reconcileEvery 每隔多少次成功刷新做一次全量对账，纠正增量可能错
	// 过的删除与漂移。
	reconcileEvery = 20
	// largeAccountItems / largeAccountReconcileInterval 大账号（指派列表
	// 巨大，全量爬取对服务端压力显著）按时间节流对账频率。
	largeAccountItems             = 50000
	largeAccountReconcileInterval = time.Hour
)

// syncSafetyMargin 覆盖禅道时间戳精度与时钟偏差。
const syncSafetyMargin = 2 * time.Minute

// Controller coordinates authenticated polling, local state, and notifications.
type Controller struct {
	refreshMu       sync.Mutex
	lifecycleMu     sync.Mutex
	gateway         ZentaoGateway
	store           SnapshotStore
	notifier        Notifier
	snapshot        Snapshot
	loggedIn        bool
	reconciles      int
	onSnapshot      func(Snapshot)
	onChanges       func(context.Context, []domain.Change)
	cancel          context.CancelFunc
	done            chan struct{}
	settingsChanged chan struct{}
}

// NewController loads the persisted baseline before polling can begin.
func NewController(ctx context.Context, gateway ZentaoGateway, store SnapshotStore, notifier Notifier) (*Controller, error) {
	snapshot, err := store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStateLoad, err)
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStateLoad, err)
	}
	return &Controller{
		gateway:         gateway,
		store:           store,
		notifier:        notifier,
		snapshot:        snapshot,
		settingsChanged: make(chan struct{}, 1),
	}, nil
}

// UpdateSettings validates and persists polling and notification preferences.
func (c *Controller) UpdateSettings(ctx context.Context, pollSeconds int, notificationsEnabled bool, popupSeconds int) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	snapshot := c.snapshot
	snapshot.PollSeconds = pollSeconds
	snapshot.NotificationsEnabled = notificationsEnabled
	snapshot.PopupSeconds = popupSeconds
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSettings, err)
	}
	if err := c.store.Save(ctx, snapshot); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	c.snapshot = snapshot
	select {
	case c.settingsChanged <- struct{}{}:
	default:
	}
	return nil
}

// Start begins an immediate refresh followed by polling until stopped.
func (c *Controller) Start(ctx context.Context, onResult func(RefreshResult), onError func(error)) {
	c.start(ctx, true, onResult, onError)
}

// StartPolling resumes periodic refreshes after an explicit Refresh has already
// populated the controller snapshot.
func (c *Controller) StartPolling(ctx context.Context, onResult func(RefreshResult), onError func(error)) {
	c.start(ctx, false, onResult, onError)
}

func (c *Controller) start(ctx context.Context, refreshImmediately bool, onResult func(RefreshResult), onError func(error)) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.cancel != nil {
		return
	}

	pollCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	go c.run(pollCtx, done, refreshImmediately, onResult, onError)
}

// Stop cancels polling and waits for the controller goroutine to exit.
func (c *Controller) Stop() {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.cancel == nil {
		return
	}
	c.cancel()
	<-c.done
	c.cancel = nil
	c.done = nil
}

func (c *Controller) run(ctx context.Context, done chan struct{}, refreshImmediately bool, onResult func(RefreshResult), onError func(error)) {
	defer close(done)
	select {
	case <-ctx.Done():
		return
	default:
	}

	report := func() {
		result, err := c.Refresh(ctx)
		if err != nil {
			if onError != nil {
				onError(err)
			}
			return
		}
		if onResult != nil {
			onResult(result)
		}
	}
	if refreshImmediately {
		report()
	}

	ticker := time.NewTicker(c.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report()
		case <-c.settingsChanged:
			ticker.Reset(c.pollInterval())
		}
	}
}

func (c *Controller) pollInterval() time.Duration {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	return time.Duration(c.snapshot.PollSeconds) * time.Second
}

// shouldReconcileNow 判断本次刷新是否回退全量对账：小账号按刷新次数
// 周期对账；大账号只按时间节流（每小时至多一次全量，避免频繁爬取压
// 垮服务端）。节流窗口随快照持久化，重启后依然有效。
func (c *Controller) shouldReconcileNow() bool {
	if len(c.snapshot.Items) > largeAccountItems {
		return c.snapshot.LastReconcile.IsZero() ||
			time.Since(c.snapshot.LastReconcile) >= largeAccountReconcileInterval
	}
	return c.reconciles >= reconcileEvery
}

// SetOnSnapshot 注册每次成功刷新后的快照回调（用于本地报表采集）。
// 回调在刷新锁之外执行，不得再调用会触发刷新的方法。
func (c *Controller) SetOnSnapshot(fn func(Snapshot)) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.onSnapshot = fn
}

// SetOnChanges registers a callback for nonempty change batches after a successful
// save, independently of popup preferences. Pass nil to unregister it.
func (c *Controller) SetOnChanges(fn func(context.Context, []domain.Change)) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.onChanges = fn
}

// CurrentSnapshot returns a copy of the current snapshot so the interface
// can render persisted items before the first refresh completes.
func (c *Controller) CurrentSnapshot() Snapshot {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	snapshot := c.snapshot
	snapshot.Items = make(map[string]domain.Item, len(c.snapshot.Items))
	for key, item := range c.snapshot.Items {
		snapshot.Items[key] = item
	}
	return snapshot
}

// Refresh fetches assigned items and persists a new baseline before reporting
// changes. 已建立基线后默认走增量同步（只抓变动条目，秒级完成）；每
// reconcileEvery 轮或增量超预算时回退全量对账。
func (c *Controller) Refresh(ctx context.Context) (RefreshResult, error) {
	c.refreshMu.Lock()
	if err := ctx.Err(); err != nil {
		c.refreshMu.Unlock()
		return RefreshResult{}, err
	}

	if !c.loggedIn {
		if err := c.gateway.Login(ctx); err != nil {
			c.refreshMu.Unlock()
			return RefreshResult{}, fmt.Errorf("login: %w", err)
		}
		c.loggedIn = true
	}

	useIncremental := c.snapshot.Initialized && !c.shouldReconcileNow()
	var items []domain.Item
	if useIncremental {
		fetched, err := c.gateway.FetchAssignedItemsUpdatedSince(ctx, c.snapshot.LastSuccess.Add(-syncSafetyMargin))
		if errors.Is(err, ErrSessionExpired) {
			c.loggedIn = false
			if err := c.gateway.Login(ctx); err != nil {
				c.refreshMu.Unlock()
				return RefreshResult{}, fmt.Errorf("re-login: %w", err)
			}
			c.loggedIn = true
			fetched, err = c.gateway.FetchAssignedItemsUpdatedSince(ctx, c.snapshot.LastSuccess.Add(-syncSafetyMargin))
			if errors.Is(err, ErrSessionExpired) {
				c.loggedIn = false
			}
		}
		if errors.Is(err, ErrIncrementalBudgetExceeded) {
			useIncremental = false
		} else if err != nil {
			c.refreshMu.Unlock()
			return RefreshResult{}, fmt.Errorf("fetch updated items: %w", err)
		} else {
			items = fetched
		}
	}
	if !useIncremental {
		var err error
		items, err = c.fetchAllAssigned(ctx)
		if err != nil {
			c.refreshMu.Unlock()
			return RefreshResult{}, err
		}
	}

	current := make(map[string]domain.Item, len(items))
	for _, item := range items {
		current[item.Key()] = item
	}

	var changes []domain.Change
	snapshot := c.snapshot
	if useIncremental {
		// 增量合并走 copy-on-write：共享的 Items map 不能原地改，
		// 否则 Save 失败时内存基线也会被污染。
		merged := make(map[string]domain.Item, len(snapshot.Items)+len(current))
		for key, item := range snapshot.Items {
			merged[key] = item
		}
		// 未完成条目 upsert 并按 DetectChanges 报告新增/状态变化；终结
		// 条目报告状态变化后从索引移除（索引只保留未完成，与全量语义
		// 一致）；首次出现的终结条目既不入索引也不通知。
		previousSub := make(map[string]domain.Item, len(current))
		currentSub := make(map[string]domain.Item, len(current))
		for key, item := range current {
			_, tracked := merged[key]
			if domain.Finished(item.Status) && !tracked {
				continue
			}
			if tracked {
				previousSub[key] = merged[key]
			}
			currentSub[key] = item
			if domain.Finished(item.Status) {
				delete(merged, key)
				continue
			}
			merged[key] = item
		}
		changes = domain.DetectChanges(previousSub, currentSub, false)
		snapshot.Items = merged
		c.reconciles++
	} else {
		changes = domain.DetectChanges(snapshot.Items, current, !snapshot.Initialized)
		snapshot.Items = current
		snapshot.LastReconcile = time.Now()
		c.reconciles = 0
	}
	snapshot.LastSuccess = time.Now()
	snapshot.Initialized = true
	if err := snapshot.Validate(); err != nil {
		c.refreshMu.Unlock()
		return RefreshResult{}, err
	}
	if err := c.store.Save(ctx, snapshot); err != nil {
		c.refreshMu.Unlock()
		return RefreshResult{}, fmt.Errorf("save snapshot: %w", err)
	}
	c.snapshot = snapshot
	onSnapshot, onChanges := c.onSnapshot, c.onChanges
	c.refreshMu.Unlock()
	if onSnapshot != nil {
		onSnapshot(snapshot)
	}
	if len(changes) > 0 {
		if onChanges != nil {
			onChanges(ctx, changes)
		}
		if snapshot.NotificationsEnabled && c.notifier != nil {
			c.notifier.Notify(ctx, changes)
		}
	}
	return RefreshResult{Snapshot: snapshot, Changes: changes}, nil
}

// fetchAllAssigned 处理登录、会话过期重试与全量抓取。
func (c *Controller) fetchAllAssigned(ctx context.Context) ([]domain.Item, error) {
	items, err := c.gateway.FetchAssignedItems(ctx)
	if errors.Is(err, ErrSessionExpired) {
		c.loggedIn = false
		if err := c.gateway.Login(ctx); err != nil {
			return nil, fmt.Errorf("re-login: %w", err)
		}
		c.loggedIn = true
		items, err = c.gateway.FetchAssignedItems(ctx)
		if errors.Is(err, ErrSessionExpired) {
			c.loggedIn = false
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetch assigned items: %w", err)
	}
	return items, nil
}

// RunItemAction 对一条事项执行生命周期操作（开始/完成/解决/关闭等）。
// 会话过期时重新登录并重试一次；成功后由调用方触发刷新以反映最新状态。
func (c *Controller) RunItemAction(ctx context.Context, item domain.Item, action string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if current, ok := c.snapshot.Items[item.Key()]; !ok || current.Status != item.Status {
		return fmt.Errorf("%s: %w", item.Key(), ErrItemChanged)
	}
	if !c.loggedIn {
		if err := c.gateway.Login(ctx); err != nil {
			return fmt.Errorf("login: %w", err)
		}
		c.loggedIn = true
	}
	err := c.gateway.RunItemAction(ctx, item.Type, item.ID, action)
	if errors.Is(err, ErrSessionExpired) {
		c.loggedIn = false
		if err := c.gateway.Login(ctx); err != nil {
			return fmt.Errorf("re-login: %w", err)
		}
		c.loggedIn = true
		err = c.gateway.RunItemAction(ctx, item.Type, item.ID, action)
		if errors.Is(err, ErrSessionExpired) {
			c.loggedIn = false
		}
	}
	if err != nil {
		return fmt.Errorf("run %s %s: %w", item.Key(), action, err)
	}
	return nil
}
