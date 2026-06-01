package web

import "context"

const RepoURL = "https://github.com/cbrgm/gopodder"

type contextKey string

const ContextKeyCSRFToken contextKey = "csrf_token"

func CSRFToken(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyCSRFToken).(string); ok {
		return v
	}
	return ""
}

type StatusData struct {
	Account    string
	IsAdmin    bool
	Stats      StatsData
	Uptime     string
	MemAlloc   string
	Goroutines int
	Version    string
	Revision   string
	BuildDate  string
	GoVersion  string
	Platform   string
	ListenAddr string
	DBBackend  string
}

type StatsData struct {
	Accounts      int64
	Users         int64
	Devices       int64
	Subscriptions int64
	Episodes      int64
}

type AccountsPageData struct {
	Account  string
	Accounts []AccountData
	Flash    string
	Error    string
}

type AccountEditData struct {
	Account     string
	EditAccount AccountData
	Users       []UserData
	APIKeys     []APIKeyData
	Flash       string
	Error       string
}

type AccountData struct {
	ID           string
	Username     string
	Role         string
	Users        int64
	CreatedAt    string
	LastLogin    string
	LastActivity string
}

type UserData struct {
	Username      string
	Devices       int64
	Subscriptions int64
	LastActivity  string
}

type UserDetailData struct {
	Account        string
	AccountID      string
	AccountName    string
	IsAdmin        bool
	ActiveTab      string
	BasePath       string
	BackURL        string
	BackLabel      string
	Username       string
	Flash          string
	Error          string
	Devices        []DeviceData
	Subscriptions  []string
	ShareToken     string
	ShareOPMLURL   string
	ShareRSSURL    string
	SharingAllowed bool
}

type DeviceData struct {
	ID           string
	Caption      string
	Type         string
	LastActivity string
}

type UserManagementData struct {
	Account        string
	IsAdmin        bool
	Users          []UserData
	Flash          string
	Error          string
	CanCreateUsers bool
}

type SettingsPageData struct {
	Account             string
	Flash               string
	Error               string
	SelfRegistration    bool
	AllowUserCreation   bool
	AllowSharing        bool
	AllowAPIKeys        bool
	MaxUsersPerAccount  int64
	MaxAPIKeys          int64
	MinPasswordLength   int64
	SessionMaxAge       int64
	EpisodeRetention    int64
	InactiveAccountDays int64
}

type AccountSelfData struct {
	Account        string
	IsAdmin        bool
	Flash          string
	Error          string
	APIKeys        []APIKeyData
	APIKeysAllowed bool
	NewKey         string
}

type APIKeyData struct {
	ID        string
	Name      string
	Prefix    string
	Role      string
	CreatedAt string
	LastUsed  string
}

type RegisterPageData struct {
	Error string
}
