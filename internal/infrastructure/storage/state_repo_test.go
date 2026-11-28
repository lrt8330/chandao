package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/storage"
)

func TestSaveInvalidSnapshotDoesNotReplaceExistingFile(t *testing.T) {
	for _, seconds := range []int{29, 301} {
		t.Run(fmt.Sprintf("poll_seconds_%d", seconds), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			original := application.DefaultSnapshot()
			if err := os.WriteFile(path, []byte("original state"), 0o600); err != nil {
				t.Fatal(err)
			}
			store := storage.FileSnapshotStore{Path: path}
			invalid := original
			invalid.PollSeconds = seconds

			if err := store.Save(context.Background(), invalid); err == nil {
				t.Fatalf("Save() with PollSeconds=%d succeeded, want error", seconds)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "original state" {
				t.Fatalf("Save() replaced existing file with %q", got)
			}
		})
	}
}

func TestSaveUsesOwnerOnlyPermissionsWhereSupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix file mode permissions")
	}

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old state"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := storage.FileSnapshotStore{Path: path}
	if err := store.Save(context.Background(), application.DefaultSnapshot()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file permissions = %04o, want 0600", got)
	}
}

func TestStateJSONContainsOnlySnapshotFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := storage.FileSnapshotStore{Path: path}
	if err := store.Save(context.Background(), application.DefaultSnapshot()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"items":                true,
		"lastSuccess":          true,
		"notificationsEnabled": true,
		"popupSeconds":         true,
		"pollSeconds":          true,
		"initialized":          true,
		"lastReconcile":        true,
	}
	if len(fields) != len(want) {
		t.Fatalf("state JSON has %d fields, want %d: %s", len(fields), len(want), data)
	}
	for name := range want {
		if _, ok := fields[name]; !ok {
			t.Errorf("state JSON is missing snapshot field %q: %s", name, data)
		}
	}
	for name := range fields {
		if !want[name] {
			t.Errorf("state JSON contains non-snapshot field %q: %s", name, data)
		}
	}
}

func TestSaveLeavesNoTemporaryFiles(t *testing.T) {
	t.Run("successful replacement", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		store := storage.FileSnapshotStore{Path: path}
		if err := store.Save(context.Background(), application.DefaultSnapshot()); err != nil {
			t.Fatal(err)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "state.json" {
			t.Fatalf("state directory entries = %v, want only state.json", entries)
		}
	})

	t.Run("failed replacement", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		store := storage.FileSnapshotStore{Path: path}
		if err := store.Save(context.Background(), application.DefaultSnapshot()); err == nil {
			t.Fatal("Save() over a directory succeeded, want error")
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "state.json" {
			t.Fatalf("failed save left files behind: %v", entries)
		}
	})
}

func TestSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var store application.SnapshotStore = storage.FileSnapshotStore{Path: path}
	want := application.Snapshot{
		Items: map[string]domain.Item{
			"task:1": {
				ID:        "1",
				Type:      domain.ItemTask,
				Title:     "Fix login",
				Project:   "Web",
				Status:    "doing",
				UpdatedAt: "2026-11-27T10:00:00Z",
				URL:       "https://example.com/task-1.html",
			},
		},
		LastSuccess:          time.Date(2026, time.November, 27, 10, 30, 0, 0, time.UTC),
		NotificationsEnabled: false,
		PollSeconds:          90,
		Initialized:          true,
	}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestLoadMissingFileReturnsDefaultsWithoutCreatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := storage.FileSnapshotStore{Path: path}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() missing file: %v", err)
	}
	if want := application.DefaultSnapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Load() created the missing state file or returned an unexpected error: %v", err)
	}
}

func TestLoadCorruptFileReturnsErrorWithoutReplacingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	corrupt := []byte(`{"items":`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	store := storage.FileSnapshotStore{Path: path}

	if _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load() corrupt file succeeded, want error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("Load() replaced corrupt file with %q, want %q", got, corrupt)
	}
}

func TestLoadInvalidPollSecondsReturnsErrorWithoutReplacingFile(t *testing.T) {
	for _, seconds := range []int{29, 301} {
		t.Run(fmt.Sprintf("poll_seconds_%d", seconds), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			data := []byte(fmt.Sprintf(`{"items":{},"pollSeconds":%d}`, seconds))
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			store := storage.FileSnapshotStore{Path: path}

			if _, err := store.Load(context.Background()); err == nil {
				t.Fatalf("Load() with PollSeconds=%d succeeded, want error", seconds)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(data) {
				t.Fatalf("Load() replaced invalid file with %q, want %q", got, data)
			}
		})
	}
}
