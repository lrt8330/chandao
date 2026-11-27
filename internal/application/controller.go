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
	ErrSessionExpired  = errors.New("session expired")
	ErrStateLoad       = errors.New("load application state")
	ErrInvalidSettings = errors.New("invalid settings")
)

// RefreshResult is the snapshot and changes produced by a successful refresh.
type RefreshResult struct {
	Snapshot Snapshot
	Changes  []domain.Change
}

// Controller coordinates authenticated polling, local state, and notifications.
type Controller struct {
	refreshMu       sync.Mutex
	lifecycleMu     sync.Mutex
	gateway         ZentaoGateway
	store           SnapshotStore
	notifier        Notifier
	snapshot        Snapshot
	loggedIn        bool
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
func (c *Controller) UpdateSettings(ctx context.Context, pollSeconds int, notificationsEnabled bool) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	snapshot := c.snapshot
	snapshot.PollSeconds = pollSeconds
	snapshot.NotificationsEnabled = notificationsEnabled
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
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.cancel != nil {
		return
	}

	pollCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	go c.run(pollCtx, done, onResult, onError)
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

func (c *Controller) run(ctx context.Context, done chan struct{}, onResult func(RefreshResult), onError func(error)) {
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
	report()

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

// Refresh fetches assigned items and persists a new baseline before reporting changes.
func (c *Controller) Refresh(ctx context.Context) (RefreshResult, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}

	if !c.loggedIn {
		if err := c.gateway.Login(ctx); err != nil {
			return RefreshResult{}, fmt.Errorf("login: %w", err)
		}
		c.loggedIn = true
	}

	items, err := c.gateway.FetchAssignedItems(ctx)
	if errors.Is(err, ErrSessionExpired) {
		c.loggedIn = false
		if err := c.gateway.Login(ctx); err != nil {
			return RefreshResult{}, fmt.Errorf("re-login: %w", err)
		}
		c.loggedIn = true
		items, err = c.gateway.FetchAssignedItems(ctx)
		if errors.Is(err, ErrSessionExpired) {
			c.loggedIn = false
		}
	}
	if err != nil {
		return RefreshResult{}, fmt.Errorf("fetch assigned items: %w", err)
	}
	current := make(map[string]domain.Item, len(items))
	for _, item := range items {
		current[item.Key()] = item
	}
	changes := domain.DetectChanges(c.snapshot.Items, current, !c.snapshot.Initialized)
	snapshot := c.snapshot
	snapshot.Items = current
	snapshot.LastSuccess = time.Now()
	snapshot.Initialized = true
	if err := snapshot.Validate(); err != nil {
		return RefreshResult{}, err
	}
	if err := c.store.Save(ctx, snapshot); err != nil {
		return RefreshResult{}, fmt.Errorf("save snapshot: %w", err)
	}
	c.snapshot = snapshot
	if snapshot.NotificationsEnabled && len(changes) > 0 {
		c.notifier.Notify(ctx, changes)
	}
	return RefreshResult{Snapshot: snapshot, Changes: changes}, nil
}
