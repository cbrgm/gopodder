package gopodder

import (
	"context"
	"time"
)

type Store interface {
	// Accounts (web UI identities)
	GetAccount(ctx context.Context, username string) (*Account, error)
	GetAccountByID(ctx context.Context, id string) (*Account, error)
	CreateAccount(ctx context.Context, id, username, pwhash, role string, createdAt time.Time) error
	UpdateAccountSession(ctx context.Context, id string, sessionID *string, now time.Time) error
	UpdateAccountLastLogin(ctx context.Context, id string, t time.Time) error
	GetAccountBySession(ctx context.Context, sessionID string) (*Account, error)
	ListAccounts(ctx context.Context) ([]Account, error)
	ListInactiveAccounts(ctx context.Context, cutoff int64) ([]Account, error)
	DeleteAccount(ctx context.Context, id string) error
	CountAccounts(ctx context.Context) (int64, error)
	UpdateAccountUsername(ctx context.Context, id, username string) error
	UpdateAccountPassword(ctx context.Context, id, pwhash string) error
	UpdateAccountRole(ctx context.Context, id, role string) error

	// Users (gPodder API identities)
	GetUser(ctx context.Context, username string) (*User, error)
	CreateUser(ctx context.Context, username, pwhash, accountID string) error
	UpdateUserPassword(ctx context.Context, username, pwhash string) error
	UpdateUserLastActivity(ctx context.Context, username string, t time.Time) error
	UpdateUserSession(ctx context.Context, username string, sessionID *string, now time.Time) error
	GetUserBySession(ctx context.Context, sessionID string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	ListUsersByAccount(ctx context.Context, accountID string) ([]User, error)
	ListUsersByAccountWithStats(ctx context.Context, accountID string) ([]UserWithStats, error)
	DeleteUser(ctx context.Context, username string) error
	DeleteUsersByAccount(ctx context.Context, accountID string) error
	SetUserShareToken(ctx context.Context, username string, token *string) error
	GetUserByShareToken(ctx context.Context, token string) (*User, error)

	// Devices
	ListDevices(ctx context.Context, username string) ([]Device, error)
	UpsertDevice(ctx context.Context, username, deviceID string, dev DeviceUpdate) error
	UpdateDeviceLastActivity(ctx context.Context, username, deviceID string, t time.Time) error
	DeleteDevice(ctx context.Context, username, deviceID string) error
	DeleteAllUserDevices(ctx context.Context, username string) error

	// Subscriptions
	GetSubscriptions(ctx context.Context, username string) ([]string, error)
	GetSubscriptionChanges(ctx context.Context, username string, since int64) (*SubscriptionChanges, error)
	UpdateSubscriptions(ctx context.Context, username string, add, remove []string, timestamp int64) error
	ReplaceSubscriptions(ctx context.Context, username string, desired []string, timestamp int64) error
	ReactivateSubscription(ctx context.Context, username, url string, timestamp int64) error
	DeleteAllUserSubscriptions(ctx context.Context, username string) error

	// Episodes
	GetEpisodes(ctx context.Context, params EpisodeQuery) ([]Episode, error)
	UpdateEpisodes(ctx context.Context, username string, episodes []Episode, timestamp int64) error
	DeleteAllUserEpisodes(ctx context.Context, username string) error
	DeleteEpisodesOlderThan(ctx context.Context, cutoff int64) (int64, error)

	// Stats
	GetStats(ctx context.Context) (Stats, error)

	// Settings
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error

	// API Keys
	CreateAPIKey(ctx context.Context, key APIKey) error
	ListAPIKeysByAccount(ctx context.Context, accountID string) ([]APIKey, error)
	GetAPIKeysByPrefix(ctx context.Context, prefix string) ([]APIKey, error)
	DeleteAPIKey(ctx context.Context, id, accountID string) error
	DeleteAPIKeysByAccount(ctx context.Context, accountID string) error
	UpdateAPIKeyLastUsed(ctx context.Context, id string, t time.Time) error
	CountAPIKeysByAccount(ctx context.Context, accountID string) (int64, error)

	// Health
	Ping(ctx context.Context) error

	Close() error
}

type Stats struct {
	Accounts      int64
	Users         int64
	Devices       int64
	Subscriptions int64
	Episodes      int64
}

type Account struct {
	ID             string
	Username       string
	PWHash         string
	Role           string
	SessionID      *string
	SessionCreated *time.Time
	CreatedAt      time.Time
	LastLogin      *time.Time
	LastActivity   *time.Time
	UserCount      int64
}

type User struct {
	Username       string
	PWHash         string
	SessionID      *string
	SessionCreated *time.Time
	AccountID      string
	LastActivity   *time.Time
	ShareToken     *string
}

type UserWithStats struct {
	Username      string
	AccountID     string
	LastActivity  *time.Time
	Devices       int64
	Subscriptions int64
}

type Device struct {
	ID            string     `json:"id"`
	Caption       string     `json:"caption"`
	Type          string     `json:"type"`
	Subscriptions int64      `json:"subscriptions"`
	LastActivity  *time.Time `json:"-"`
}

type DeviceUpdate struct {
	Caption *string `json:"caption"`
	Type    *string `json:"type"`
}

type SubscriptionChanges struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type Episode struct {
	Podcast   string  `json:"podcast"`
	Episode   string  `json:"episode"`
	Device    *string `json:"device,omitempty"`
	Timestamp *string `json:"timestamp,omitempty"`
	GUID      *string `json:"guid,omitempty"`
	Action    string  `json:"action"`
	Started   *int64  `json:"started,omitempty"`
	Position  *int64  `json:"position,omitempty"`
	Total     *int64  `json:"total,omitempty"`
}

type EpisodeQuery struct {
	Username string
	Podcast  *string
	Device   *string
	Since    int64
}

type APIKey struct {
	ID        string
	AccountID string
	Name      string
	Prefix    string
	Hash      string
	Role      string
	CreatedAt time.Time
	LastUsed  *time.Time
}
