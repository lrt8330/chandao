package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/zentao"
)

// fixedGateway 只返回固定数据，供跨层测试演练控制器 + 存储 + 报表库。
type fixedGateway struct {
	items   []domain.Item
	delta   []domain.Item
	actions []string
}

func (g *fixedGateway) Login(context.Context) error { return nil }

func (g *fixedGateway) FetchAssignedItems(context.Context) ([]domain.Item, error) {
	return g.items, nil
}

func (g *fixedGateway) FetchAssignedItemsUpdatedSince(context.Context, time.Time) ([]domain.Item, error) {
	return g.delta, nil
}

func (g *fixedGateway) RunItemAction(_ context.Context, itemType domain.ItemType, id, action string) error {
	g.actions = append(g.actions, string(itemType)+":"+id+":"+action)
	return nil
}

type memoryCredentials struct{ values map[string]string }

func (m *memoryCredentials) Get(key string) (string, bool, error) {
	value, found := m.values[key]
	return value, found, nil
}

func (m *memoryCredentials) Set(key, value string) error {
	m.values[key] = value
	return nil
}

func (m *memoryCredentials) Delete(key string) error {
	delete(m.values, key)
	return nil
}

// 同一数据目录下的不同账号互不共享快照、历史与保存的凭据。
func TestAccountSessionsIsolateSnapshotsAndHistory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	const server = "http://zentao.example/biz/"

	aliceItem := domain.Item{ID: "1", Type: domain.ItemTask, Title: "Alice 的任务", Status: "wait"}
	bobItem := domain.Item{ID: "5", Type: domain.ItemBug, Title: "Bob 的缺陷", Status: "active"}

	aliceGateway := &fixedGateway{
		items: []domain.Item{aliceItem},
		delta: []domain.Item{{ID: "1", Type: domain.ItemTask, Title: "Alice 的任务", Status: "doing"}, {ID: "2", Type: domain.ItemTask, Title: "Alice 的新任务", Status: "wait"}},
	}
	alice, err := openAccountSession(ctx, root, server, "alice", aliceGateway, nil)
	if err != nil {
		t.Fatalf("open alice: %v", err)
	}
	defer alice.close()
	if _, err := alice.controller.Refresh(ctx); err != nil {
		t.Fatalf("alice baseline: %v", err)
	}
	if _, err := alice.controller.Refresh(ctx); err != nil {
		t.Fatalf("alice delta: %v", err)
	}

	bob, err := openAccountSession(ctx, root, server, "bob", &fixedGateway{items: []domain.Item{bobItem}, delta: []domain.Item{bobItem}}, nil)
	if err != nil {
		t.Fatalf("open bob: %v", err)
	}
	defer bob.close()
	if _, err := bob.controller.Refresh(ctx); err != nil {
		t.Fatalf("bob baseline: %v", err)
	}

	// 快照隔离：Bob 的首轮基线不能带出 Alice 的任务。
	for key, item := range bob.controller.CurrentSnapshot().Items {
		if item.Title != bobItem.Title {
			t.Fatalf("bob snapshot leaked %q from another account", item.Title)
		}
		_ = key
	}
	if len(bob.controller.CurrentSnapshot().Items) != 1 {
		t.Fatalf("bob items = %v", bob.controller.CurrentSnapshot().Items)
	}

	// 历史隔离：Alice 的变化写入自己的库，Bob 的库为空。
	at := time.Time{}
	aliceEntries, err := alice.history.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("alice history: %v", err)
	}
	if len(aliceEntries) == 0 {
		t.Fatal("alice change history is empty")
	}
	bobEntries, err := bob.history.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("bob history: %v", err)
	}
	if len(bobEntries) != 0 {
		t.Fatalf("bob history contains %d entries from another account", len(bobEntries))
	}
	_ = at

	// 报表快照来自真实刷新：Alice 的总量从 1 变到 2。
	points, err := alice.history.Snapshots(ctx, time.Time{}, 10)
	if err != nil {
		t.Fatalf("alice snapshots: %v", err)
	}
	if len(points) < 2 {
		t.Fatalf("alice snapshots = %v, want one per distinct total", points)
	}
	if points[len(points)-1].Total != 2 || points[len(points)-1].TasksTotal != 2 {
		t.Fatalf("latest snapshot = %+v, want 2 tasks", points[len(points)-1])
	}
	if points, err := bob.history.Snapshots(ctx, time.Time{}, 10); err != nil || len(points) == 0 || points[0].BugsActive != 1 {
		t.Fatalf("bob snapshots = %v, %v", points, err)
	}
}

// 账号或实例不同就必须落在不同的状态目录。
func TestProfilePathsSeparateAccountsAndInstances(t *testing.T) {
	aliceID, err := instanceID("http://zentao.example/biz/")
	if err != nil {
		t.Fatalf("instanceID: %v", err)
	}
	otherID, err := instanceID("http://zentao.example/other/")
	if err != nil {
		t.Fatalf("instanceID: %v", err)
	}
	if aliceID == otherID {
		t.Fatal("different instances share a profile id")
	}
	// 同一实例、账号大小写不同也必须是不同目录（禅道账号区分大小写）。
	root := t.TempDir()
	a := filepath.Join(root, "profiles", aliceID, profileHash("alice"))
	b := filepath.Join(root, "profiles", aliceID, profileHash("Alice"))
	if a == b {
		t.Fatal("different accounts share a state directory")
	}
}

// 凭据按服务器绑定：同一账号换服务器不能复用旧密码。
func TestLoginSecretBindsToServer(t *testing.T) {
	store := &memoryCredentials{values: map[string]string{}}
	if err := saveLogin(store, "http://zentao.example/biz/", loginSecret{Account: "alice", Password: "one"}); err != nil {
		t.Fatalf("saveLogin: %v", err)
	}
	secret, found, err := readLogin(store, "http://zentao.example/biz/")
	if err != nil || !found || secret.Account != "alice" || secret.Password != "one" {
		t.Fatalf("readLogin = %+v, %v, %v", secret, found, err)
	}
	if _, found, _ := readLogin(store, "http://other.example/biz/"); found {
		t.Fatal("credentials leaked across servers")
	}
	if _, err := instanceID("ftp://zentao.example/biz/"); !errors.Is(err, zentao.ErrInvalidBaseURL) {
		t.Fatalf("invalid scheme error = %v", err)
	}
}
