// Package storage implements local persistence without UI dependencies.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"chandao/zentaotray/internal/application"
)

// FileSnapshotStore persists one application snapshot as JSON.
type FileSnapshotStore struct {
	Path string
}

// Load reads the snapshot, returning first-run defaults when no file exists.
func (s FileSnapshotStore) Load(ctx context.Context) (application.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return application.Snapshot{}, err
	}

	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return application.DefaultSnapshot(), nil
	}
	if err != nil {
		return application.Snapshot{}, fmt.Errorf("read state file: %w", err)
	}

	var snapshot application.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return application.Snapshot{}, fmt.Errorf("decode state file: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return application.Snapshot{}, fmt.Errorf("validate state file: %w", err)
	}
	return snapshot, nil
}

// Save atomically replaces the state file with a validated JSON snapshot.
func (s FileSnapshotStore) Save(ctx context.Context, snapshot application.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("validate snapshot: %w", err)
	}
	if s.Path == "" {
		return fmt.Errorf("state file path is empty")
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(s.Path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()

	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary state file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync temporary state file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.Path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}
