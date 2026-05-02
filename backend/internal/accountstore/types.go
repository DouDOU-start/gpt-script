package accountstore

import (
	"context"
	"time"
)

type Account struct {
	ID                int64
	Email             string
	Password          string
	Status            string
	Workspaces        string
	Cookies           string
	AccessToken       string
	OAuthAccessToken  string
	OAuthRefreshToken string
	OAuthExpiresAt    string
	OAuthPlanType     string
	OrganizationID    string
	LastMode          string
	LastError         string
	LastRunID         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Query struct {
	Statuses []string
	Email    string
	Limit    int
	Offset   int
}

type Proxy struct {
	ID            int64
	Host          string
	Port          int
	Protocol      string
	Username      string
	Password      string
	Enabled       bool
	Status        string
	FailCount     int
	LastUsedAt    time.Time
	LastCheckedAt time.Time
	CreatedAt     time.Time
}

type ProxyQuery struct {
	Status string
	Limit  int
	Offset int
}

type ResultUpdate struct {
	Email             string
	Mode              string
	Success           bool
	ErrorMessage      string
	RunID             string
	Workspaces        string
	Cookies           string
	AccessToken       string
	OAuthAccessToken  string
	OAuthRefreshToken string
	OAuthExpiresAt    string
	OAuthPlanType     string
	OrganizationID    string
}

type ImportSummary struct {
	Imported int
	Updated  int
	Skipped  int
	Invalid  int
}

type Store interface {
	Migrate(ctx context.Context) error
	UpsertAccount(ctx context.Context, account Account) (bool, error)
	ListAccounts(ctx context.Context, query Query) ([]Account, error)
	CountAccounts(ctx context.Context, query Query) (int, error)
	CreateProxy(ctx context.Context, proxy Proxy) (Proxy, error)
	ListProxies(ctx context.Context, query ProxyQuery) ([]Proxy, error)
	CountProxies(ctx context.Context, query ProxyQuery) (int, error)
	GetProxy(ctx context.Context, id int64) (Proxy, bool, error)
	DeleteProxy(ctx context.Context, id int64) (bool, error)
	SetProxyEnabled(ctx context.Context, id int64, enabled bool) (bool, error)
	BatchDeleteProxies(ctx context.Context, ids []int64) (int64, error)
	BatchSetProxyEnabled(ctx context.Context, ids []int64, enabled bool) (int64, error)
	UpdateProxyCheck(ctx context.Context, id int64, success bool, checkedAt time.Time) error
	UpdateFromResult(ctx context.Context, update ResultUpdate) error
	ImportLegacy(ctx context.Context, sourcePath string) (ImportSummary, error)
	Close() error
}
