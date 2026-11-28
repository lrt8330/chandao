// Package notifyhistory 持久化通知历史：每次刷新检出的变化写入本地
// SQLite，供界面回查。单进程单写，全部访问经互斥锁串行。
package notifyhistory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Entry 是一条已通知的变化记录。
type Entry struct {
	ID        int64
	Time      time.Time
	Kind      string // new / status
	ItemType  string
	ItemID    string
	Title     string
	Project   string
	OldStatus string
	NewStatus string
	Read      bool
}

// Store 是通知历史的 SQLite 存储。
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open 打开（必要时创建）指定路径的通知历史库并完成建表迁移。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open notify history: %w", err)
	}
	schema := snapshotsSchema + `
CREATE TABLE IF NOT EXISTS notifications (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         TEXT NOT NULL,
	kind       TEXT NOT NULL,
	item_type  TEXT NOT NULL,
	item_id    TEXT NOT NULL,
	title      TEXT NOT NULL,
	project    TEXT NOT NULL DEFAULT '',
	old_status TEXT NOT NULL DEFAULT '',
	new_status TEXT NOT NULL DEFAULT '',
	read       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_notifications_read ON notifications(read, id DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_ts ON notifications(ts DESC);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate notify history: %w", err)
	}
	return &Store{db: db}, nil
}

// appendBatchLimit 单批次落库的明细上限；超出部分以一条汇总记录代替，
// 避免 10 万级变化风暴一次性灌入海量行。
const appendBatchLimit = 5000

// Append 把一次刷新检出的变化批量写入历史。超过上限时写一条汇总记录。
func (s *Store) Append(ctx context.Context, at time.Time, changes []Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin append: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	insert := "INSERT INTO notifications (ts, kind, item_type, item_id, title, project, old_status, new_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	timestamp := at.Format(time.RFC3339Nano)
	if len(changes) > appendBatchLimit {
		summary := fmt.Sprintf("批量变化 %d 条（仅保留前 %d 条明细）", len(changes), appendBatchLimit)
		if _, err := tx.ExecContext(ctx, insert, timestamp, "summary", "", "", summary, "", "", ""); err != nil {
			return fmt.Errorf("append summary: %w", err)
		}
		changes = changes[:appendBatchLimit]
	}
	for _, entry := range changes {
		if _, err := tx.ExecContext(ctx, insert,
			entryTime(timestamp, entry), entry.Kind, entry.ItemType, entry.ItemID,
			entry.Title, entry.Project, entry.OldStatus, entry.NewStatus); err != nil {
			return fmt.Errorf("append %s %s: %w", entry.ItemType, entry.ItemID, err)
		}
	}
	return tx.Commit()
}

// entryTime 单条记录沿用批次时间戳，保持同批顺序与时间一致。
func entryTime(batchTimestamp string, entry Entry) string {
	if entry.Time.IsZero() {
		return batchTimestamp
	}
	return entry.Time.Format(time.RFC3339Nano)
}

// Recent 返回最近的 limit 条记录（新在前）。
func (s *Store) Recent(ctx context.Context, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, ts, kind, item_type, item_id, title, project, old_status, new_status, read FROM notifications ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("query recent notifications: %w", err)
	}
	defer rows.Close()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// UnreadCount 返回未读通知数。
func (s *Store) UnreadCount(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE read = 0").Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

// MarkAllRead 把全部通知标记为已读。
func (s *Store) MarkAllRead(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, "UPDATE notifications SET read = 1 WHERE read = 0"); err != nil {
		return fmt.Errorf("mark notifications read: %w", err)
	}
	return nil
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var entries []Entry
	for rows.Next() {
		var entry Entry
		var timestamp, kind, itemType, itemID, title, project, oldStatus, newStatus string
		var read int
		if err := rows.Scan(&entry.ID, &timestamp, &kind, &itemType, &itemID, &title, &project, &oldStatus, &newStatus, &read); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		entry.Time, _ = time.Parse(time.RFC3339Nano, timestamp)
		entry.Kind, entry.ItemType, entry.ItemID = kind, itemType, itemID
		entry.Title, entry.Project, entry.OldStatus, entry.NewStatus = title, project, oldStatus, newStatus
		entry.Read = read != 0
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return entries, nil
}

// Close 关闭底层连接。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// Trim 把历史裁剪到最近的 keep 条，防止长期积累无限增长。
func (s *Store) Trim(ctx context.Context, keep int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM notifications WHERE id NOT IN (SELECT id FROM notifications ORDER BY id DESC LIMIT ?)", keep); err != nil {
		return fmt.Errorf("trim notifications: %w", err)
	}
	return nil
}

// KindLabel 返回变化类型的中文名。
func KindLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "new":
		return "新增"
	case "status":
		return "状态变化"
	case "summary":
		return "批量变化"
	}
	return kind
}
