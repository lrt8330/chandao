package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/notifyhistory"
	"chandao/zentaotray/internal/infrastructure/storage"
	"chandao/zentaotray/internal/infrastructure/zentao"
)

// Preserve the deployment path and account case: one host may serve several instances.
func instanceID(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", zentao.ErrInvalidBaseURL
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if port == "80" && u.Scheme == "http" || port == "443" && u.Scheme == "https" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return profileHash(u.Scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/") + "/"), nil
}

func profileHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type loginSecret struct{ Account, Password string }

func saveLogin(store application.CredentialStore, server string, secret loginSecret) error {
	id, err := instanceID(server)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	return store.Set("login-v2:"+id, string(encoded))
}

func readLogin(store application.CredentialStore, server string) (loginSecret, bool, error) {
	id, err := instanceID(server)
	if err != nil {
		return loginSecret{}, false, err
	}
	value, found, err := store.Get("login-v2:" + id)
	if err != nil || !found {
		return loginSecret{}, false, err
	}
	var secret loginSecret
	err = json.Unmarshal([]byte(value), &secret)
	return secret, true, err
}

type accountSession struct {
	account    string
	controller *application.Controller
	history    *notifyhistory.Store
}

// Legacy files have no owner metadata. Never claim them on first login.
func openAccountSession(ctx context.Context, root, server, account string, gateway application.ZentaoGateway, notifier application.Notifier) (*accountSession, error) {
	id, err := instanceID(server)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "profiles", id, profileHash(account))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, application.ErrStateLoad
	}
	controller, err := application.NewController(ctx, gateway, storage.FileSnapshotStore{Path: filepath.Join(dir, "state.json")}, notifier)
	if err != nil {
		return nil, err
	}
	s := &accountSession{account: account, controller: controller}
	// If SQLite is unavailable, lists and popups remain usable.
	if history, err := notifyhistory.Open(filepath.Join(dir, "notify.db")); err == nil {
		s.history = history
		_ = history.Trim(ctx, 10000)
		controller.SetOnSnapshot(func(snapshot application.Snapshot) { recordSnapshot(ctx, history, snapshot) })
		controller.SetOnChanges(func(ctx context.Context, changes []domain.Change) { recordChanges(ctx, history, changes) })
	}
	return s, nil
}

func (s *accountSession) close() {
	s.controller.Stop()
	if s.history != nil {
		_ = s.history.Close()
	}
}

// recordSnapshot 把一次成功刷新的总量快照写入该账号的报表库。
func recordSnapshot(parent context.Context, history *notifyhistory.Store, snapshot application.Snapshot) {
	if history == nil {
		return
	}
	point := notifyhistory.SnapshotPoint{}
	for _, item := range snapshot.Items {
		switch {
		case item.Type == domain.ItemTask:
			point.TasksTotal++
			switch strings.ToLower(item.Status) {
			case "doing":
				point.TasksDoing++
			case "wait":
				point.TasksWait++
			case "pause":
				point.TasksPause++
			}
		case item.Type == domain.ItemBug && strings.EqualFold(item.Status, "active"):
			point.BugsActive++
		}
	}
	point.Total = len(snapshot.Items)
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	_ = history.AppendSnapshot(ctx, snapshot.LastSuccess, point)
}

func recordChanges(ctx context.Context, history *notifyhistory.Store, changes []domain.Change) {
	at := time.Now()
	entries := make([]notifyhistory.Entry, 0, len(changes))
	for _, change := range changes {
		entries = append(entries, notifyhistory.Entry{Time: at, Kind: string(change.Kind), ItemType: string(change.Item.Type), ItemID: change.Item.ID, Title: change.Item.Title, Project: change.Item.Project, OldStatus: change.Old, NewStatus: change.New})
	}
	_ = history.Append(ctx, at, entries)
}
