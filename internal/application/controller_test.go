package application_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
)

type fakeGateway struct {
	mu            sync.Mutex
	items         []domain.Item
	updatedItems  []domain.Item
	updatedErr    error
	updatedCalls  int
	fetchErrors   []error
	loginErrors   []error
	loginCalls    int
	fetchCalls    int
	fetchStarted  chan struct{}
	fetchContinue chan struct{}

	actionCalls    int
	actionErrs     []error
	lastActionItem domain.Item
	lastAction     string
}

// RunItemAction 记录调用并按预设错误序列返回。
func (g *fakeGateway) RunItemAction(ctx context.Context, itemType domain.ItemType, id, action string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.actionCalls++
	g.lastActionItem = domain.Item{ID: id, Type: itemType}
	g.lastAction = action
	if len(g.actionErrs) > 0 {
		err := g.actionErrs[0]
		g.actionErrs = g.actionErrs[1:]
		return err
	}
	return nil
}

func (g *fakeGateway) Login(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loginCalls++
	if len(g.loginErrors) > 0 {
		err := g.loginErrors[0]
		g.loginErrors = g.loginErrors[1:]
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *fakeGateway) FetchAssignedItemsUpdatedSince(ctx context.Context, since time.Time) ([]domain.Item, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.updatedCalls++
	if len(g.fetchErrors) > 0 {
		err := g.fetchErrors[0]
		g.fetchErrors = g.fetchErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if g.updatedErr != nil {
		return nil, g.updatedErr
	}
	if g.updatedItems != nil {
		return append([]domain.Item(nil), g.updatedItems...), nil
	}
	return append([]domain.Item(nil), g.items...), nil
}

func (g *fakeGateway) FetchAssignedItems(ctx context.Context) ([]domain.Item, error) {
	g.mu.Lock()
	g.fetchCalls++
	started, continueFetch := g.fetchStarted, g.fetchContinue
	var err error
	if len(g.fetchErrors) > 0 {
		err = g.fetchErrors[0]
		g.fetchErrors = g.fetchErrors[1:]
	}
	items := append([]domain.Item(nil), g.items...)
	g.mu.Unlock()

	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if continueFetch != nil {
		select {
		case <-continueFetch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return items, err
}

func (g *fakeGateway) counts() (int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.loginCalls, g.fetchCalls
}

type fakeStore struct {
	mu        sync.Mutex
	snapshot  application.Snapshot
	loadErr   error
	saveErr   error
	saveCalls int
}

func (s *fakeStore) Load(ctx context.Context) (application.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSnapshot(s.snapshot), s.loadErr
}

func (s *fakeStore) Save(ctx context.Context, snapshot application.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCalls++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.snapshot = cloneSnapshot(snapshot)
	return nil
}

func (s *fakeStore) state() (application.Snapshot, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSnapshot(s.snapshot), s.saveCalls
}

type fakeNotifier struct {
	mu      sync.Mutex
	batches [][]domain.Change
	hook    func()
}

func (n *fakeNotifier) Notify(ctx context.Context, changes []domain.Change) {
	n.mu.Lock()
	n.batches = append(n.batches, append([]domain.Change(nil), changes...))
	hook := n.hook
	n.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func (n *fakeNotifier) notifications() [][]domain.Change {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][]domain.Change(nil), n.batches...)
}

func cloneSnapshot(snapshot application.Snapshot) application.Snapshot {
	items := make(map[string]domain.Item, len(snapshot.Items))
	for key, item := range snapshot.Items {
		items[key] = item
	}
	snapshot.Items = items
	return snapshot
}

func itemMap(items ...domain.Item) map[string]domain.Item {
	result := make(map[string]domain.Item, len(items))
	for _, item := range items {
		result[item.Key()] = item
	}
	return result
}

func testItem(id string, itemType domain.ItemType, status string) domain.Item {
	return domain.Item{
		ID:        id,
		Type:      itemType,
		Title:     "Item " + id,
		Project:   "Web",
		Status:    status,
		UpdatedAt: "2026-11-27T10:00:00Z",
		URL:       "https://example.com/item-" + id + ".html",
	}
}

func newTestController(t *testing.T, gateway *fakeGateway, store *fakeStore, notifier *fakeNotifier) *application.Controller {
	t.Helper()
	controller, err := application.NewController(context.Background(), gateway, store, notifier)
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	return controller
}

func TestRefreshNotifiesForNewAndStatusChangesAfterSave(t *testing.T) {
	oldTask := testItem("1", domain.ItemTask, "doing")
	updatedTask := oldTask
	updatedTask.Status = "done"
	newBug := testItem("2", domain.ItemBug, "active")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(oldTask)
	gateway := &fakeGateway{items: []domain.Item{updatedTask, newBug}}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	notifier.hook = func() {
		saved, saveCalls := store.state()
		if _, exists := saved.Items[updatedTask.Key()]; exists {
			t.Errorf("Notify() ran before the finished task was removed from the index")
		}
		if saveCalls != 1 || saved.Items[newBug.Key()].Status != "active" {
			t.Errorf("Notify() ran before updated snapshot was saved")
		}
	}
	controller := newTestController(t, gateway, store, notifier)

	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	want := []domain.Change{
		{Kind: domain.ChangeNew, Item: newBug},
		{Kind: domain.ChangeStatus, Item: updatedTask, Old: "doing", New: "done"},
	}
	if !reflect.DeepEqual(result.Changes, want) {
		t.Errorf("Refresh() changes = %#v, want %#v", result.Changes, want)
	}
	if got := notifier.notifications(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("Notify() batches = %#v, want one batch %#v", got, want)
	}
}

func TestRefreshNotificationCanUpdateSettings(t *testing.T) {
	oldItem := testItem("1", domain.ItemTask, "doing")
	newItem := testItem("2", domain.ItemBug, "active")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(oldItem)
	gateway := &fakeGateway{items: []domain.Item{newItem}}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)
	settingsDone := make(chan error, 1)
	notifier.hook = func() {
		settingsDone <- controller.UpdateSettings(context.Background(), 30, true, 12)
	}
	refreshDone := make(chan error, 1)
	go func() {
		_, err := controller.Refresh(context.Background())
		refreshDone <- err
	}()

	select {
	case err := <-settingsDone:
		if err != nil {
			t.Fatalf("UpdateSettings() from Notify() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Refresh() deadlocked: Notify() could not call UpdateSettings() while refreshMu was held")
	}
	select {
	case err := <-refreshDone:
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Refresh() did not finish after Notify() updated settings")
	}
	saved, saveCalls := store.state()
	if saveCalls != 2 {
		t.Errorf("Save() calls = %d, want 2", saveCalls)
	}
	if saved.PollSeconds != 30 || !saved.NotificationsEnabled || saved.PopupSeconds != 12 || saved.Items[newItem.Key()] != newItem {
		t.Errorf("final snapshot = %#v, want updated settings and new baseline", saved)
	}
}

func TestStopCancelsInFlightRefreshAndWaitsForErrorCallback(t *testing.T) {
	fetchStarted := make(chan struct{}, 1)
	continueFetch := make(chan struct{})
	gateway := &fakeGateway{fetchStarted: fetchStarted, fetchContinue: continueFetch}
	controller := newTestController(t, gateway, &fakeStore{snapshot: application.DefaultSnapshot()}, &fakeNotifier{})
	errorsSeen := make(chan error, 1)
	controller.Start(context.Background(), nil, func(err error) {
		errorsSeen <- err
	})
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("Start() did not begin fetching")
	}

	stopped := make(chan struct{})
	go func() {
		controller.Stop()
		close(stopped)
	}()
	select {
	case err := <-errorsSeen:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("onError() = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel the in-flight refresh")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() returned before the controller goroutine exited")
	}
}

func TestStartRefreshesImmediatelyAndStopWaits(t *testing.T) {
	item := testItem("1", domain.ItemTask, "doing")
	fetchStarted := make(chan struct{}, 1)
	continueFetch := make(chan struct{})
	gateway := &fakeGateway{
		items:         []domain.Item{item},
		fetchStarted:  fetchStarted,
		fetchContinue: continueFetch,
	}
	store := &fakeStore{snapshot: application.DefaultSnapshot()}
	controller := newTestController(t, gateway, store, &fakeNotifier{})
	results := make(chan application.RefreshResult, 1)
	errorsSeen := make(chan error, 1)

	controller.Start(context.Background(), func(result application.RefreshResult) {
		results <- result
	}, func(err error) {
		errorsSeen <- err
	})
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("Start() did not begin an immediate refresh")
	}
	select {
	case result := <-results:
		t.Fatalf("onResult ran before immediate fetch completed: %#v", result)
	default:
	}
	close(continueFetch)
	select {
	case result := <-results:
		if !result.Snapshot.Initialized || result.Snapshot.Items[item.Key()] != item {
			t.Errorf("immediate result snapshot = %#v, want initialized item", result.Snapshot)
		}
	case err := <-errorsSeen:
		t.Fatalf("immediate refresh error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("Start() did not report immediate refresh result")
	}

	stopped := make(chan struct{})
	go func() {
		controller.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return")
	}
	controller.Stop()
}

func TestStartPollingSkipsImmediateRefresh(t *testing.T) {
	item := testItem("1", domain.ItemTask, "doing")
	gateway := &fakeGateway{items: []domain.Item{item}}
	store := &fakeStore{snapshot: application.DefaultSnapshot()}
	controller := newTestController(t, gateway, store, &fakeNotifier{})
	if _, err := controller.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	controller.StartPolling(context.Background(), nil, nil)
	time.Sleep(50 * time.Millisecond)
	loginCalls, fetchCalls := gateway.counts()
	if loginCalls != 1 || fetchCalls != 1 {
		t.Errorf("gateway calls after StartPolling = (%d logins, %d fetches), want (1, 1)", loginCalls, fetchCalls)
	}
	controller.Stop()
}

func TestConcurrentRefreshIsSerialized(t *testing.T) {
	fetchStarted := make(chan struct{}, 2)
	continueFetch := make(chan struct{})
	gateway := &fakeGateway{fetchStarted: fetchStarted, fetchContinue: continueFetch}
	store := &fakeStore{snapshot: application.DefaultSnapshot()}
	controller := newTestController(t, gateway, store, &fakeNotifier{})
	firstDone := make(chan error, 1)
	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)

	go func() {
		_, err := controller.Refresh(context.Background())
		firstDone <- err
	}()
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("first Refresh() did not start fetching")
	}
	go func() {
		close(secondStarted)
		_, err := controller.Refresh(context.Background())
		secondDone <- err
	}()
	<-secondStarted

	select {
	case <-fetchStarted:
		t.Fatal("second Refresh() fetched while the first refresh was blocked")
	case <-time.After(50 * time.Millisecond):
	}
	close(continueFetch)
	for name, done := range map[string]<-chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("%s Refresh() error = %v", name, err)
			}
		case <-time.After(time.Second):
			t.Errorf("%s Refresh() did not finish", name)
		}
	}
	loginCalls, fetchCalls := gateway.counts()
	if loginCalls != 1 || fetchCalls+gateway.updatedCalls != 2 {
		t.Errorf("gateway calls = (%d logins, %d fetches, %d incremental), want (1, 2 total)", loginCalls, fetchCalls, gateway.updatedCalls)
	}
}

func TestUpdateSettingsWaitsForInFlightRefresh(t *testing.T) {
	fetchStarted := make(chan struct{}, 1)
	continueFetch := make(chan struct{})
	gateway := &fakeGateway{fetchStarted: fetchStarted, fetchContinue: continueFetch}
	store := &fakeStore{snapshot: application.DefaultSnapshot()}
	controller := newTestController(t, gateway, store, &fakeNotifier{})
	refreshDone := make(chan error, 1)
	settingsStarted := make(chan struct{})
	settingsDone := make(chan error, 1)

	go func() {
		_, err := controller.Refresh(context.Background())
		refreshDone <- err
	}()
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("Refresh() did not start fetching")
	}
	go func() {
		close(settingsStarted)
		settingsDone <- controller.UpdateSettings(context.Background(), 30, false, 0)
	}()
	<-settingsStarted
	select {
	case err := <-settingsDone:
		t.Fatalf("UpdateSettings() completed during refresh: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(continueFetch)
	for name, done := range map[string]<-chan error{"refresh": refreshDone, "settings": settingsDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("%s error = %v", name, err)
			}
		case <-time.After(time.Second):
			t.Errorf("%s did not finish", name)
		}
	}
	saved, saveCalls := store.state()
	if saveCalls != 2 {
		t.Errorf("Save() calls = %d, want 2", saveCalls)
	}
	if saved.PollSeconds != 30 || saved.NotificationsEnabled || saved.PopupSeconds != 0 || !saved.Initialized {
		t.Errorf("final snapshot = %#v, want initialized snapshot with settings (30, false)", saved)
	}
}

func TestUpdateSettingsSaveFailureDoesNotChangeSettings(t *testing.T) {
	saveErr := errors.New("read-only state")
	store := &fakeStore{snapshot: application.DefaultSnapshot(), saveErr: saveErr}
	controller := newTestController(t, &fakeGateway{}, store, &fakeNotifier{})

	if err := controller.UpdateSettings(context.Background(), 30, false, 8); !errors.Is(err, saveErr) {
		t.Fatalf("UpdateSettings() error = %v, want wrapped save error", err)
	}
	saved, _ := store.state()
	if saved.PollSeconds != 60 || !saved.NotificationsEnabled {
		t.Errorf("settings after failed save = (%d, %t), want unchanged (60, true)", saved.PollSeconds, saved.NotificationsEnabled)
	}
}

func TestUpdateSettingsValidatesAndPersistsWithoutChangingBaseline(t *testing.T) {
	item := testItem("1", domain.ItemTask, "doing")
	lastSuccess := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(item)
	snapshot.LastSuccess = lastSuccess
	store := &fakeStore{snapshot: snapshot}
	controller := newTestController(t, &fakeGateway{}, store, &fakeNotifier{})

	for _, seconds := range []int{-1, 0, 29, 301} {
		if err := controller.UpdateSettings(context.Background(), seconds, false, 8); !errors.Is(err, application.ErrInvalidSettings) {
			t.Errorf("UpdateSettings(%d) error = %v, want ErrInvalidSettings", seconds, err)
		}
	}
	if _, saveCalls := store.state(); saveCalls != 0 {
		t.Fatalf("Save() calls after invalid settings = %d, want 0", saveCalls)
	}

	for _, settings := range []struct {
		seconds       int
		notifications bool
		popupSeconds  int
	}{{seconds: 30, notifications: false, popupSeconds: 0}, {seconds: 300, notifications: true, popupSeconds: 60}} {
		if err := controller.UpdateSettings(context.Background(), settings.seconds, settings.notifications, settings.popupSeconds); err != nil {
			t.Fatalf("UpdateSettings(%d, %t, %d) error = %v", settings.seconds, settings.notifications, settings.popupSeconds, err)
		}
	}
	saved, saveCalls := store.state()
	if saveCalls != 2 {
		t.Errorf("Save() calls = %d, want 2", saveCalls)
	}
	if saved.PollSeconds != 300 || !saved.NotificationsEnabled || saved.PopupSeconds != 60 {
		t.Errorf("saved settings = (%d, %t, %d), want (300, true, 60)", saved.PollSeconds, saved.NotificationsEnabled, saved.PopupSeconds)
	}
	if !saved.Initialized || !saved.LastSuccess.Equal(lastSuccess) || !reflect.DeepEqual(saved.Items, snapshot.Items) {
		t.Errorf("UpdateSettings() changed baseline: %#v", saved)
	}
}

func TestNewControllerRejectsUnloadableOrInvalidState(t *testing.T) {
	loadErr := errors.New("decode state file: invalid character")
	invalidSnapshot := application.DefaultSnapshot()
	invalidSnapshot.PollSeconds = 0
	tests := []struct {
		name  string
		store *fakeStore
	}{
		{name: "load failure", store: &fakeStore{loadErr: loadErr}},
		{name: "invalid loaded settings", store: &fakeStore{snapshot: invalidSnapshot}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &fakeGateway{}
			controller, err := application.NewController(context.Background(), gateway, tt.store, &fakeNotifier{})
			if err == nil {
				t.Fatal("NewController() error = nil, want startup failure")
			}
			if controller != nil {
				t.Errorf("NewController() controller = %#v, want nil", controller)
			}
			if !errors.Is(err, application.ErrStateLoad) {
				t.Errorf("NewController() error = %v, want ErrStateLoad", err)
			}
			if tt.store.loadErr != nil && !errors.Is(err, loadErr) {
				t.Errorf("NewController() error = %v, want wrapped load cause", err)
			}
			loginCalls, fetchCalls := gateway.counts()
			if loginCalls != 0 || fetchCalls != 0 {
				t.Errorf("gateway calls = (%d logins, %d fetches), want no calls", loginCalls, fetchCalls)
			}
		})
	}
}

func TestRefreshNotificationsDisabledStillReportsChanges(t *testing.T) {
	oldItem := testItem("1", domain.ItemTask, "doing")
	newItem := testItem("2", domain.ItemBug, "active")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.NotificationsEnabled = false
	snapshot.Items = itemMap(oldItem)
	gateway := &fakeGateway{items: []domain.Item{newItem}}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)

	events := &fakeNotifier{}
	controller.SetOnChanges(events.Notify)

	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	want := []domain.Change{{Kind: domain.ChangeNew, Item: newItem}}
	if !reflect.DeepEqual(result.Changes, want) {
		t.Errorf("Refresh() changes = %#v, want %#v", result.Changes, want)
	}
	if got := events.notifications(); !reflect.DeepEqual(got, [][]domain.Change{want}) {
		t.Errorf("onChanges batches = %#v, want one batch %#v", got, want)
	}
	if _, saveCalls := store.state(); saveCalls != 1 {
		t.Errorf("Save() calls = %d, want 1", saveCalls)
	}
	if got := notifier.notifications(); len(got) != 0 {
		t.Errorf("Notify() calls = %d, want 0 when disabled", len(got))
	}
}

func TestRefreshSaveFailureSkipsNotificationAndPreservesBaseline(t *testing.T) {
	oldItem := testItem("1", domain.ItemTask, "doing")
	newItem := testItem("2", domain.ItemBug, "active")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(oldItem)
	saveErr := errors.New("disk full")
	gateway := &fakeGateway{items: []domain.Item{newItem}}
	store := &fakeStore{snapshot: snapshot, saveErr: saveErr}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)

	if _, err := controller.Refresh(context.Background()); !errors.Is(err, saveErr) {
		t.Fatalf("Refresh() error = %v, want wrapped save error", err)
	}
	if got := notifier.notifications(); len(got) != 0 {
		t.Fatalf("Notify() calls after failed save = %d, want 0", len(got))
	}

	store.mu.Lock()
	store.saveErr = nil
	store.mu.Unlock()
	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	want := []domain.Change{{Kind: domain.ChangeNew, Item: newItem}}
	if !reflect.DeepEqual(result.Changes, want) {
		t.Errorf("second Refresh() changes = %#v, want %#v from preserved baseline", result.Changes, want)
	}
	if got := notifier.notifications(); len(got) != 1 {
		t.Errorf("Notify() calls = %d, want 1 after successful save", len(got))
	}
}

func TestRefreshRepeatedSessionExpiryDoesNotSaveOrNotify(t *testing.T) {
	item := testItem("1", domain.ItemTask, "doing")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(item)
	gateway := &fakeGateway{fetchErrors: []error{
		fmt.Errorf("first fetch: %w", application.ErrSessionExpired),
		fmt.Errorf("retry fetch: %w", application.ErrSessionExpired),
	}}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)

	_, err := controller.Refresh(context.Background())
	if !errors.Is(err, application.ErrSessionExpired) {
		t.Fatalf("Refresh() error = %v, want session expiry", err)
	}
	loginCalls, fetchCalls := gateway.counts()
	if loginCalls != 2 || fetchCalls+gateway.updatedCalls != 2 {
		t.Errorf("gateway calls = (%d logins, %d fetches, %d incremental), want (2, 2 total)", loginCalls, fetchCalls, gateway.updatedCalls)
	}
	if _, saveCalls := store.state(); saveCalls != 0 {
		t.Errorf("Save() calls = %d, want 0", saveCalls)
	}
	if got := notifier.notifications(); len(got) != 0 {
		t.Errorf("Notify() calls = %d, want 0", len(got))
	}

	gateway.mu.Lock()
	gateway.fetchErrors = append(gateway.fetchErrors, nil)
	gateway.mu.Unlock()
	if _, err := controller.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() after repeated expiry error = %v", err)
	}
	loginCalls, fetchCalls = gateway.counts()
	if loginCalls != 3 || fetchCalls+gateway.updatedCalls != 3 {
		t.Errorf("gateway calls after recovery = (%d logins, %d fetches, %d incremental), want (3, 3 total)", loginCalls, fetchCalls, gateway.updatedCalls)
	}
}

func TestRefreshSessionExpiryLogsInOnceAndRetries(t *testing.T) {
	oldItem := testItem("1", domain.ItemTask, "doing")
	newItem := testItem("2", domain.ItemBug, "active")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(oldItem)
	gateway := &fakeGateway{
		items:       []domain.Item{newItem},
		fetchErrors: []error{fmt.Errorf("fetch page: %w", application.ErrSessionExpired), nil},
	}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)

	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	want := []domain.Change{{Kind: domain.ChangeNew, Item: newItem}}
	if !reflect.DeepEqual(result.Changes, want) {
		t.Errorf("Refresh() changes = %#v, want %#v", result.Changes, want)
	}
	loginCalls, fetchCalls := gateway.counts()
	if loginCalls != 2 || fetchCalls+gateway.updatedCalls != 2 {
		t.Errorf("gateway calls = (%d logins, %d fetches, %d incremental), want (2, 2 total)", loginCalls, fetchCalls, gateway.updatedCalls)
	}
	if _, saveCalls := store.state(); saveCalls != 1 {
		t.Errorf("Save() calls = %d, want 1", saveCalls)
	}
	if got := notifier.notifications(); len(got) != 1 {
		t.Errorf("Notify() calls = %d, want 1", len(got))
	}
}

func TestRefreshFetchFailurePreservesSnapshotWithoutSaveOrNotification(t *testing.T) {
	oldItem := testItem("1", domain.ItemTask, "doing")
	newItem := testItem("2", domain.ItemBug, "active")
	lastSuccess := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(oldItem)
	snapshot.LastSuccess = lastSuccess
	fetchErr := errors.New("parse assigned page")
	gateway := &fakeGateway{items: []domain.Item{newItem}, fetchErrors: []error{fetchErr}}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)

	_, err := controller.Refresh(context.Background())
	if !errors.Is(err, fetchErr) {
		t.Fatalf("Refresh() error = %v, want wrapped fetch error", err)
	}
	saved, saveCalls := store.state()
	if saveCalls != 0 {
		t.Errorf("Save() calls = %d, want 0", saveCalls)
	}
	if !reflect.DeepEqual(saved, snapshot) {
		t.Errorf("saved snapshot = %#v, want unchanged %#v", saved, snapshot)
	}
	if got := notifier.notifications(); len(got) != 0 {
		t.Errorf("Notify() calls = %d, want 0", len(got))
	}
}

func TestRefreshDuplicateDoesNotNotify(t *testing.T) {
	item := testItem("1", domain.ItemTask, "doing")
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(item)
	gateway := &fakeGateway{items: []domain.Item{item, item}}
	store := &fakeStore{snapshot: snapshot}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)

	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(result.Changes) != 0 {
		t.Errorf("Refresh() changes = %#v, want no changes", result.Changes)
	}
	if got := notifier.notifications(); len(got) != 0 {
		t.Errorf("Notify() calls = %d, want 0", len(got))
	}
	if _, saveCalls := store.state(); saveCalls != 1 {
		t.Errorf("Save() calls = %d, want 1 for successful duplicate refresh", saveCalls)
	}
}

func TestRefreshFirstBaselineSavesWithoutNotification(t *testing.T) {
	item := testItem("1", domain.ItemTask, "doing")
	gateway := &fakeGateway{items: []domain.Item{item}}
	store := &fakeStore{snapshot: application.DefaultSnapshot()}
	notifier := &fakeNotifier{}
	controller := newTestController(t, gateway, store, notifier)
	before := time.Now()

	result, err := controller.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Refresh() changes = %#v, want no changes for first baseline", result.Changes)
	}
	if !result.Snapshot.Initialized {
		t.Error("Refresh() snapshot Initialized = false, want true")
	}
	if result.Snapshot.LastSuccess.Before(before) || result.Snapshot.LastSuccess.After(time.Now()) {
		t.Errorf("Refresh() LastSuccess = %v, want current time", result.Snapshot.LastSuccess)
	}
	if got := result.Snapshot.Items[item.Key()]; got != item {
		t.Errorf("Refresh() item = %#v, want %#v", got, item)
	}
	saved, saveCalls := store.state()
	if saveCalls != 1 || !saved.Initialized || saved.Items[item.Key()] != item {
		t.Errorf("saved snapshot = %#v after %d saves, want initialized baseline", saved, saveCalls)
	}
	if got := notifier.notifications(); len(got) != 0 {
		t.Errorf("Notify() calls = %d, want 0", len(got))
	}
	loginCalls, fetchCalls := gateway.counts()
	if loginCalls != 1 || fetchCalls != 1 {
		t.Errorf("gateway calls = (%d logins, %d fetches), want (1, 1)", loginCalls, fetchCalls)
	}
}

// TestLargeAccountReconcileThrottledByTime 验证大账号（超过 5 万条）的
// 全量对账按时间节流：1 小时窗口内持续增量、不触发全量爬取；窗口过后
// 回退一次全量对账。
func TestLargeAccountReconcileThrottledByTime(t *testing.T) {
	largeItems := make([]domain.Item, 0, 50001)
	for index := 1; index <= 50001; index++ {
		largeItems = append(largeItems, domain.Item{
			ID: strconv.Itoa(index), Type: domain.ItemBug, Title: "大批量", Status: "active",
		})
	}

	// 阶段一：对账时间在节流窗口内 → 连续 21 轮全部走增量。
	throttledSnapshot := application.DefaultSnapshot()
	throttledSnapshot.Initialized = true
	throttledSnapshot.LastSuccess = time.Now().Add(-2 * time.Minute)
	throttledSnapshot.LastReconcile = time.Now()
	throttledSnapshot.Items = itemMap(largeItems...)
	gatewayA := &fakeGateway{items: largeItems, updatedItems: largeItems}
	storeA := &fakeStore{snapshot: cloneSnapshot(throttledSnapshot)}
	controllerA := newTestController(t, gatewayA, storeA, &fakeNotifier{})
	for round := 0; round < 21; round++ {
		if _, err := controllerA.Refresh(context.Background()); err != nil {
			t.Fatalf("throttled refresh %d: %v", round, err)
		}
	}
	if gatewayA.fetchCalls != 0 {
		t.Fatalf("full crawls during throttle window = %d, want 0", gatewayA.fetchCalls)
	}
	if gatewayA.updatedCalls != 21 {
		t.Fatalf("incremental fetches = %d, want 21", gatewayA.updatedCalls)
	}

	// 阶段二：对账时间拨回 2 小时前 → 下一次刷新回退全量对账。
	staleSnapshot := throttledSnapshot
	staleSnapshot.LastReconcile = time.Now().Add(-2 * time.Hour)
	gatewayB := &fakeGateway{items: largeItems, updatedItems: largeItems}
	storeB := &fakeStore{snapshot: cloneSnapshot(staleSnapshot)}
	controllerB := newTestController(t, gatewayB, storeB, &fakeNotifier{})

	result, err := controllerB.Refresh(context.Background())
	if err != nil {
		t.Fatalf("reconcile refresh: %v", err)
	}
	if gatewayB.fetchCalls == 0 {
		t.Fatal("reconcile refresh did not run the full crawl")
	}
	if len(result.Snapshot.Items) != len(largeItems) {
		t.Fatalf("reconcile items = %d, want %d", len(result.Snapshot.Items), len(largeItems))
	}
	if saved, _ := storeB.state(); saved.LastReconcile.IsZero() {
		t.Fatal("lastReconcile not persisted after reconciliation")
	}
}

func TestRunItemActionRejectsStaleItemBeforeGatewayCall(t *testing.T) {
	selected := testItem("108", domain.ItemTask, "wait")
	updated := selected
	updated.Status = "doing"
	for _, tc := range []struct {
		name  string
		items map[string]domain.Item
	}{
		{"removed", itemMap()},
		{"different status", itemMap(updated)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := application.DefaultSnapshot()
			snapshot.Initialized = true
			snapshot.Items = tc.items
			gateway := &fakeGateway{}
			controller := newTestController(t, gateway, &fakeStore{snapshot: snapshot}, &fakeNotifier{})
			if err := controller.RunItemAction(context.Background(), selected, "start"); !errors.Is(err, application.ErrItemChanged) {
				t.Fatalf("RunItemAction() error = %v, want ErrItemChanged", err)
			}
			if gateway.loginCalls != 0 || gateway.actionCalls != 0 {
				t.Errorf("stale selection caused %d logins and %d actions", gateway.loginCalls, gateway.actionCalls)
			}
		})
	}
}

// TestControllerRunsItemActionWithSessionRetry 会话过期时重登录并重试一次。
func TestControllerRunsItemActionWithSessionRetry(t *testing.T) {
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(testItem("108", domain.ItemTask, ""))
	store := &fakeStore{snapshot: snapshot}
	gateway := &fakeGateway{
		loginErrors: []error{nil},
		actionErrs:  []error{application.ErrSessionExpired},
	}
	controller, err := application.NewController(context.Background(), gateway, store, nil)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.RunItemAction(context.Background(), domain.Item{ID: "108", Type: domain.ItemTask}, "start"); err != nil {
		t.Fatalf("RunItemAction: %v", err)
	}
	if gateway.loginCalls != 2 { // 首次登录 + 过期后重登录
		t.Errorf("login calls = %d, want 2", gateway.loginCalls)
	}
	if gateway.actionCalls != 2 {
		t.Errorf("action calls = %d, want 2 (one retry)", gateway.actionCalls)
	}
	if gateway.lastAction != "start" || gateway.lastActionItem.ID != "108" {
		t.Errorf("last action = %s %v", gateway.lastAction, gateway.lastActionItem)
	}
}

// TestControllerRunsItemActionWithoutLogin 首次调用前会先登录。
func TestControllerRunsItemActionWithoutLogin(t *testing.T) {
	snapshot := application.DefaultSnapshot()
	snapshot.Initialized = true
	snapshot.Items = itemMap(testItem("6", domain.ItemBug, ""))
	store := &fakeStore{snapshot: snapshot}
	gateway := &fakeGateway{}
	controller, err := application.NewController(context.Background(), gateway, store, nil)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.RunItemAction(context.Background(), domain.Item{ID: "6", Type: domain.ItemBug}, "resolve"); err != nil {
		t.Fatalf("RunItemAction: %v", err)
	}
	if gateway.loginCalls != 1 {
		t.Errorf("login calls = %d, want 1", gateway.loginCalls)
	}
}
