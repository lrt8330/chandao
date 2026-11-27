package application_test

import (
	"testing"

	"chandao/zentaotray/internal/application"
)

func TestDefaultSnapshot(t *testing.T) {
	got := application.DefaultSnapshot()

	if got.PollSeconds != 60 {
		t.Errorf("PollSeconds = %d, want 60", got.PollSeconds)
	}
	if !got.NotificationsEnabled {
		t.Error("NotificationsEnabled = false, want true")
	}
	if got.Initialized {
		t.Error("Initialized = true, want false")
	}
	if !got.LastSuccess.IsZero() {
		t.Errorf("LastSuccess = %v, want zero", got.LastSuccess)
	}
	if got.Items == nil || len(got.Items) != 0 {
		t.Errorf("Items = %#v, want an empty map", got.Items)
	}
}

func TestSnapshotValidatePollSeconds(t *testing.T) {
	for _, seconds := range []int{30, 60, 300} {
		snapshot := application.DefaultSnapshot()
		snapshot.PollSeconds = seconds
		if err := snapshot.Validate(); err != nil {
			t.Errorf("Validate() with PollSeconds=%d: %v", seconds, err)
		}
	}

	for _, seconds := range []int{-1, 0, 29, 301} {
		snapshot := application.DefaultSnapshot()
		snapshot.PollSeconds = seconds
		if err := snapshot.Validate(); err == nil {
			t.Errorf("Validate() with PollSeconds=%d succeeded, want error", seconds)
		}
	}
}
