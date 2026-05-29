package gopodder

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type mockStore struct {
	accounts      map[string]*Account
	users         map[string]*User
	devices       map[string][]Device
	subscriptions map[string][]string // keyed by username
	episodes      map[string][]Episode
	settings      map[string]string
	apiKeys       []APIKey
}

func newMockStore() *mockStore {
	return &mockStore{
		accounts:      make(map[string]*Account),
		users:         make(map[string]*User),
		devices:       make(map[string][]Device),
		subscriptions: make(map[string][]string),
		episodes:      make(map[string][]Episode),
		settings:      make(map[string]string),
	}
}

// Accounts

func (m *mockStore) GetAccount(_ context.Context, username string) (*Account, error) {
	for _, a := range m.accounts {
		if a.Username == username {
			return a, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockStore) GetAccountByID(_ context.Context, id string) (*Account, error) {
	a, ok := m.accounts[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return a, nil
}

func (m *mockStore) CreateAccount(_ context.Context, id, username, pwhash, role string, _ time.Time) error {
	m.accounts[id] = &Account{ID: id, Username: username, PWHash: pwhash, Role: role}
	return nil
}

func (m *mockStore) UpdateAccountSession(_ context.Context, id string, sessionID *string, now time.Time) error {
	if a, ok := m.accounts[id]; ok {
		a.SessionID = sessionID
		if sessionID != nil {
			a.SessionCreated = &now
		} else {
			a.SessionCreated = nil
		}
	}
	return nil
}

func (m *mockStore) UpdateAccountLastLogin(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (m *mockStore) GetAccountBySession(_ context.Context, sessionID string) (*Account, error) {
	for _, a := range m.accounts {
		if a.SessionID != nil && *a.SessionID == sessionID {
			return a, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockStore) ListAccounts(_ context.Context) ([]Account, error) {
	accounts := make([]Account, 0, len(m.accounts))
	for _, a := range m.accounts {
		accounts = append(accounts, *a)
	}
	return accounts, nil
}

func (m *mockStore) DeleteAccount(_ context.Context, id string) error {
	delete(m.accounts, id)
	return nil
}

func (m *mockStore) ListInactiveAccounts(_ context.Context, _ int64) ([]Account, error) {
	return nil, nil
}

func (m *mockStore) CountAccounts(_ context.Context) (int64, error) {
	return int64(len(m.accounts)), nil
}

func (m *mockStore) UpdateAccountUsername(_ context.Context, id, username string) error {
	if a, ok := m.accounts[id]; ok {
		a.Username = username
	}
	return nil
}

func (m *mockStore) UpdateAccountPassword(_ context.Context, id, pwhash string) error {
	if a, ok := m.accounts[id]; ok {
		a.PWHash = pwhash
	}
	return nil
}

func (m *mockStore) UpdateAccountRole(_ context.Context, id, role string) error {
	if a, ok := m.accounts[id]; ok {
		a.Role = role
	}
	return nil
}

// Users

func (m *mockStore) GetUser(_ context.Context, username string) (*User, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return u, nil
}

func (m *mockStore) CreateUser(_ context.Context, username, pwhash, accountID string) error {
	m.users[username] = &User{Username: username, PWHash: pwhash, AccountID: accountID}
	return nil
}

func (m *mockStore) UpdateUserPassword(_ context.Context, username, pwhash string) error {
	if u, ok := m.users[username]; ok {
		u.PWHash = pwhash
	}
	return nil
}

func (m *mockStore) UpdateUserLastActivity(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (m *mockStore) UpdateUserSession(_ context.Context, username string, sessionID *string, now time.Time) error {
	if u, ok := m.users[username]; ok {
		u.SessionID = sessionID
		if sessionID != nil {
			u.SessionCreated = &now
		} else {
			u.SessionCreated = nil
		}
	}
	return nil
}

func (m *mockStore) GetUserBySession(_ context.Context, sessionID string) (*User, error) {
	for _, u := range m.users {
		if u.SessionID != nil && *u.SessionID == sessionID {
			return u, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockStore) ListUsers(_ context.Context) ([]User, error) {
	users := make([]User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, *u)
	}
	return users, nil
}

func (m *mockStore) ListUsersByAccount(_ context.Context, accountID string) ([]User, error) {
	var users []User
	for _, u := range m.users {
		if u.AccountID == accountID {
			users = append(users, *u)
		}
	}
	return users, nil
}

func (m *mockStore) ListUsersByAccountWithStats(_ context.Context, accountID string) ([]UserWithStats, error) {
	var users []UserWithStats
	for _, u := range m.users {
		if u.AccountID == accountID {
			users = append(users, UserWithStats{
				Username:      u.Username,
				AccountID:     u.AccountID,
				LastActivity:  u.LastActivity,
				Devices:       int64(len(m.devices[u.Username])),
				Subscriptions: int64(len(m.subscriptions[u.Username])),
			})
		}
	}
	return users, nil
}

func (m *mockStore) DeleteUser(_ context.Context, username string) error {
	delete(m.users, username)
	return nil
}

func (m *mockStore) DeleteUsersByAccount(_ context.Context, accountID string) error {
	for name, u := range m.users {
		if u.AccountID == accountID {
			delete(m.users, name)
		}
	}
	return nil
}

func (m *mockStore) SetUserShareToken(_ context.Context, username string, token *string) error {
	if u, ok := m.users[username]; ok {
		u.ShareToken = token
	}
	return nil
}

func (m *mockStore) GetUserByShareToken(_ context.Context, token string) (*User, error) {
	for _, u := range m.users {
		if u.ShareToken != nil && *u.ShareToken == token {
			return u, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

// Devices

func (m *mockStore) ListDevices(_ context.Context, username string) ([]Device, error) {
	return m.devices[username], nil
}

func (m *mockStore) UpsertDevice(_ context.Context, username, deviceID string, dev DeviceUpdate) error {
	d := Device{ID: deviceID, Type: ptrStringOr(dev.Type, "other")}
	if dev.Caption != nil {
		d.Caption = *dev.Caption
	}
	devs := m.devices[username]
	for i, existing := range devs {
		if existing.ID == deviceID {
			devs[i] = d
			m.devices[username] = devs
			return nil
		}
	}
	m.devices[username] = append(devs, d)
	return nil
}

func (m *mockStore) UpdateDeviceLastActivity(_ context.Context, _ string, _ string, _ time.Time) error {
	return nil
}

func (m *mockStore) DeleteDevice(_ context.Context, username, deviceID string) error {
	devs := m.devices[username]
	for i, d := range devs {
		if d.ID == deviceID {
			m.devices[username] = append(devs[:i], devs[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockStore) DeleteAllUserDevices(_ context.Context, username string) error {
	delete(m.devices, username)
	return nil
}

// Subscriptions

func (m *mockStore) GetSubscriptions(_ context.Context, username string) ([]string, error) {
	return m.subscriptions[username], nil
}

func (m *mockStore) GetSubscriptionChanges(_ context.Context, _ string, _ int64) (*SubscriptionChanges, error) {
	return &SubscriptionChanges{Add: []string{}, Remove: []string{}}, nil
}

func (m *mockStore) UpdateSubscriptions(_ context.Context, username string, add, remove []string, _ int64) error {
	subs := m.subscriptions[username]
	for _, u := range add {
		subs = append(subs, u)
	}
	rmSet := make(map[string]struct{})
	for _, u := range remove {
		rmSet[u] = struct{}{}
	}
	filtered := subs[:0]
	for _, u := range subs {
		if _, ok := rmSet[u]; !ok {
			filtered = append(filtered, u)
		}
	}
	m.subscriptions[username] = filtered
	return nil
}

func (m *mockStore) ReplaceSubscriptions(_ context.Context, username string, desired []string, _ int64) error {
	m.subscriptions[username] = desired
	return nil
}

func (m *mockStore) ReactivateSubscription(_ context.Context, _ string, _ string, _ int64) error {
	return nil
}


func (m *mockStore) DeleteAllUserSubscriptions(_ context.Context, username string) error {
	delete(m.subscriptions, username)
	return nil
}

// Episodes

func (m *mockStore) GetEpisodes(_ context.Context, params EpisodeQuery) ([]Episode, error) {
	return m.episodes[params.Username], nil
}

func (m *mockStore) UpdateEpisodes(_ context.Context, username string, episodes []Episode, _ int64) error {
	m.episodes[username] = append(m.episodes[username], episodes...)
	return nil
}

func (m *mockStore) DeleteAllUserEpisodes(_ context.Context, username string) error {
	delete(m.episodes, username)
	return nil
}

func (m *mockStore) DeleteEpisodesOlderThan(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

// API Keys

func (m *mockStore) CreateAPIKey(_ context.Context, key APIKey) error {
	m.apiKeys = append(m.apiKeys, key)
	return nil
}

func (m *mockStore) ListAPIKeysByAccount(_ context.Context, accountID string) ([]APIKey, error) {
	var keys []APIKey
	for _, k := range m.apiKeys {
		if k.AccountID == accountID {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockStore) GetAPIKeysByPrefix(_ context.Context, prefix string) ([]APIKey, error) {
	var keys []APIKey
	for _, k := range m.apiKeys {
		if k.Prefix == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockStore) DeleteAPIKey(_ context.Context, id, accountID string) error {
	for i, k := range m.apiKeys {
		if k.ID == id && k.AccountID == accountID {
			m.apiKeys = append(m.apiKeys[:i], m.apiKeys[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockStore) DeleteAPIKeysByAccount(_ context.Context, accountID string) error {
	filtered := m.apiKeys[:0]
	for _, k := range m.apiKeys {
		if k.AccountID != accountID {
			filtered = append(filtered, k)
		}
	}
	m.apiKeys = filtered
	return nil
}

func (m *mockStore) UpdateAPIKeyLastUsed(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (m *mockStore) CountAPIKeysByAccount(_ context.Context, accountID string) (int64, error) {
	var count int64
	for _, k := range m.apiKeys {
		if k.AccountID == accountID {
			count++
		}
	}
	return count, nil
}

// Stats

func (m *mockStore) GetStats(_ context.Context) (Stats, error) {
	var subs int64
	for _, urls := range m.subscriptions {
		subs += int64(len(urls))
	}
	return Stats{
		Accounts:      int64(len(m.accounts)),
		Users:         int64(len(m.users)),
		Devices:       int64(len(m.devices)),
		Subscriptions: subs,
	}, nil
}

// Settings

func (m *mockStore) GetSetting(_ context.Context, key string) (string, error) {
	v, ok := m.settings[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func (m *mockStore) SetSetting(_ context.Context, key, value string) error {
	m.settings[key] = value
	return nil
}

func (m *mockStore) Ping(_ context.Context) error { return nil }
func (m *mockStore) Close() error                  { return nil }

func newTestAPI(store Store) *API {
	if ms, ok := store.(*mockStore); ok {
		ms.accounts["admin-id"] = &Account{ID: "admin-id", Username: "admin", PWHash: hashPassword("admin"), Role: RoleAdmin}
	}
	logger := slog.Default()
	return NewAPI(logger, store, noopMetrics{}, BuildInfo{
		Version:   "test",
		Revision:  "abc123",
		BuildDate: "2024-01-01",
		GoVersion: "go1.22.0",
		Platform:  "linux/amd64",
	}, "127.0.0.1:8080", "sqlite")
}

func authedRequest(method, path, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.SetBasicAuth("testuser", "testpass")
	return r
}
