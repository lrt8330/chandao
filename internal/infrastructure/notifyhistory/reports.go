// 报表数据层：快照序列（时间演变）与事件统计/查询。
package notifyhistory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SnapshotPoint 是一次刷新时刻的待办总量快照。
type SnapshotPoint struct {
	ID         int64
	Time       time.Time
	TasksTotal int
	TasksDoing int
	TasksWait  int
	TasksPause int
	BugsActive int
	Total      int
}

// EventStatsRow 是按天聚合的变化统计。
type EventStatsRow struct {
	Day    string // 2006-01-02
	New    int
	Status int
}

// EventFilter 过滤事件明细。零值字段表示不过滤。
type EventFilter struct {
	Since   time.Time
	Kind    string // "" 全部 / new / status / summary
	Keyword string // 匹配标题或编号（LIKE）
	Limit   int    // 默认 100
}

// migrateV2 追加快照表（幂等）。
const snapshotsSchema = `
CREATE TABLE IF NOT EXISTS snapshots (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	ts          TEXT NOT NULL,
	tasks_total INTEGER NOT NULL,
	tasks_doing INTEGER NOT NULL DEFAULT 0,
	tasks_wait  INTEGER NOT NULL DEFAULT 0,
	tasks_pause INTEGER NOT NULL DEFAULT 0,
	bugs_active INTEGER NOT NULL DEFAULT 0,
	total       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snapshots_ts ON snapshots(ts DESC);
`

// AppendSnapshot 记录一次刷新的总量快照；与最近一条完全相同时跳过，
// 避免数据不变时产生重复点。
func (s *Store) AppendSnapshot(ctx context.Context, at time.Time, point SnapshotPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var lastTotal, lastTasks, lastBugs int
	err := s.db.QueryRowContext(ctx,
		"SELECT total, tasks_total, bugs_active FROM snapshots ORDER BY id DESC LIMIT 1").
		Scan(&lastTotal, &lastTasks, &lastBugs)
	if err == nil && lastTotal == point.Total && lastTasks == point.TasksTotal && lastBugs == point.BugsActive {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO snapshots (ts, tasks_total, tasks_doing, tasks_wait, tasks_pause, bugs_active, total) VALUES (?, ?, ?, ?, ?, ?, ?)",
		at.Format(time.RFC3339Nano), point.TasksTotal, point.TasksDoing, point.TasksWait, point.TasksPause, point.BugsActive, point.Total)
	if err != nil {
		return fmt.Errorf("append snapshot: %w", err)
	}
	return nil
}

// Snapshots 返回 since 以来的快照序列（时间升序），至多 limit 条。
func (s *Store) Snapshots(ctx context.Context, since time.Time, limit int) ([]SnapshotPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, ts, tasks_total, tasks_doing, tasks_wait, tasks_pause, bugs_active, total FROM snapshots WHERE ts >= ? ORDER BY id ASC LIMIT ?",
		since.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()
	var points []SnapshotPoint
	for rows.Next() {
		var point SnapshotPoint
		var timestamp string
		if err := rows.Scan(&point.ID, &timestamp, &point.TasksTotal, &point.TasksDoing, &point.TasksWait, &point.TasksPause, &point.BugsActive, &point.Total); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		point.Time, _ = time.Parse(time.RFC3339Nano, timestamp)
		points = append(points, point)
	}
	return points, rows.Err()
}

// EventStats 按天聚合 since 以来的变化计数（新在前）。
func (s *Store) EventStats(ctx context.Context, since time.Time) ([]EventStatsRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		"SELECT substr(ts, 1, 10) AS day, kind, COUNT(*) FROM notifications WHERE ts >= ? AND kind IN ('new', 'status') GROUP BY day, kind ORDER BY day",
		since.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query event stats: %w", err)
	}
	defer rows.Close()
	byDay := map[string]*EventStatsRow{}
	var days []string
	for rows.Next() {
		var day, kind string
		var count int
		if err := rows.Scan(&day, &kind, &count); err != nil {
			return nil, fmt.Errorf("scan event stats: %w", err)
		}
		row, ok := byDay[day]
		if !ok {
			row = &EventStatsRow{Day: day}
			byDay[day] = row
			days = append(days, day)
		}
		if kind == "new" {
			row.New = count
		} else {
			row.Status = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortStrings(days)
	out := make([]EventStatsRow, 0, len(days))
	for _, day := range days {
		out = append(out, *byDay[day])
	}
	return out, nil
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// QueryEvents 按过滤条件查询事件明细（新在前）。
func (s *Store) QueryEvents(ctx context.Context, filter EventFilter) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query := "SELECT id, ts, kind, item_type, item_id, title, project, old_status, new_status, read FROM notifications"
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if !filter.Since.IsZero() {
		conditions = append(conditions, "ts >= ?")
		args = append(args, filter.Since.Format(time.RFC3339Nano))
	}
	if filter.Kind != "" {
		conditions = append(conditions, "kind = ?")
		args = append(args, filter.Kind)
	}
	if filter.Keyword != "" {
		conditions = append(conditions, "(title LIKE ? OR item_id LIKE ?)")
		keyword := "%" + filter.Keyword + "%"
		args = append(args, keyword, keyword)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}
