package application

import (
	"fmt"
	"time"

	"chandao/zentaotray/internal/domain"
)

const (
	minPollSeconds  = 30
	maxPollSeconds  = 300
	maxPopupSeconds = 60
)

// Snapshot is the local baseline and polling configuration. Credentials and
// session data are deliberately not part of the persisted state.
type Snapshot struct {
	Items                map[string]domain.Item `json:"items"`
	LastSuccess          time.Time              `json:"lastSuccess"`
	NotificationsEnabled bool                   `json:"notificationsEnabled"`
	PopupSeconds         int                    `json:"popupSeconds"`
	PollSeconds          int                    `json:"pollSeconds"`
	Initialized          bool                   `json:"initialized"`
}

// Validate rejects settings that cannot be used safely.
func (s Snapshot) Validate() error {
	if s.PollSeconds < minPollSeconds || s.PollSeconds > maxPollSeconds {
		return fmt.Errorf("poll seconds must be between %d and %d, got %d", minPollSeconds, maxPollSeconds, s.PollSeconds)
	}
	if s.PopupSeconds < 0 || s.PopupSeconds > maxPopupSeconds {
		return fmt.Errorf("popup seconds must be between 0 and %d, got %d", maxPopupSeconds, s.PopupSeconds)
	}
	return nil
}

// DefaultSnapshot returns the settings for a first run.
func DefaultSnapshot() Snapshot {
	return Snapshot{
		Items:                make(map[string]domain.Item),
		NotificationsEnabled: true,
		PopupSeconds:         8,
		PollSeconds:          60,
	}
}
