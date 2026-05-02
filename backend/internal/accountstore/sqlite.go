package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteTimeFormat = time.RFC3339Nano

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	dsn := path
	if !strings.Contains(path, ":memory:") {
		var err error
		dsn, err = sqliteDSN(path, false)
		if err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	stmts := []string{
		`create table if not exists schema_migrations (
			version integer primary key,
			applied_at text not null
		)`,
		`create table if not exists accounts (
			id integer primary key autoincrement,
			email text not null unique,
			password text not null,
			status text not null default 'active',
			workspaces text,
			cookies text,
			access_token text,
			oauth_access_token text,
			oauth_refresh_token text,
			oauth_expires_at text,
			oauth_plan_type text,
			organization_id text,
			last_mode text,
			last_error text,
			last_run_id text,
			created_at text not null,
			updated_at text not null
		)`,
		`create index if not exists idx_accounts_status on accounts(status)`,
		`create index if not exists idx_accounts_updated_at on accounts(updated_at)`,
		`create table if not exists proxies (
			id integer primary key autoincrement,
			host text not null,
			port integer not null,
			protocol text not null default 'http',
			username text,
			password text,
			enabled integer not null default 1,
			status text not null default 'active',
			fail_count integer not null default 0,
			last_used_at text,
			last_checked_at text,
			created_at text not null
		)`,
		`create index if not exists idx_proxies_status on proxies(status)`,
		`create unique index if not exists idx_proxies_unique on proxies(protocol, host, port, coalesce(username, ''), coalesce(password, ''))`,
		`insert or ignore into schema_migrations(version, applied_at) values (1, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.dropPhoneVerifiedColumnIfExists(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `update accounts set status = 'active', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') where status in ('new', 'failed') or trim(status) = ''`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `insert or ignore into schema_migrations(version, applied_at) values (2, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) UpsertAccount(ctx context.Context, account Account) (bool, error) {
	email := strings.TrimSpace(account.Email)
	if email == "" || account.Password == "" {
		return false, errors.New("email and password are required")
	}
	status := strings.ToLower(strings.TrimSpace(account.Status))
	if status == "" || status == "new" || status == "failed" {
		status = "active"
	}
	now := time.Now().UTC()
	createdAt := account.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := account.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}

	result, err := s.db.ExecContext(ctx, `insert into accounts (
		email, password, status, workspaces, cookies, access_token,
		oauth_access_token, oauth_refresh_token, oauth_expires_at, oauth_plan_type,
		organization_id, last_mode, last_error, last_run_id, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(email) do update set
		password=excluded.password,
		status=excluded.status,
		workspaces=excluded.workspaces,
		cookies=excluded.cookies,
		access_token=excluded.access_token,
		oauth_access_token=excluded.oauth_access_token,
		oauth_refresh_token=excluded.oauth_refresh_token,
		oauth_expires_at=excluded.oauth_expires_at,
		oauth_plan_type=excluded.oauth_plan_type,
		organization_id=excluded.organization_id,
		last_mode=excluded.last_mode,
		last_error=excluded.last_error,
		last_run_id=excluded.last_run_id,
		updated_at=excluded.updated_at`,
		email, account.Password, status, nullString(account.Workspaces), nullString(account.Cookies), nullString(account.AccessToken),
		nullString(account.OAuthAccessToken), nullString(account.OAuthRefreshToken), nullString(account.OAuthExpiresAt), nullString(account.OAuthPlanType),
		nullString(account.OrganizationID), nullString(account.LastMode), nullString(account.LastError), nullString(account.LastRunID),
		formatTime(createdAt), formatTime(updatedAt))
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func (s *SQLiteStore) ListAccounts(ctx context.Context, query Query) ([]Account, error) {
	where, args := accountWhere(query)
	limit := ""
	if query.Limit > 0 {
		limit = " limit ?"
		args = append(args, query.Limit)
	}
	if query.Offset > 0 {
		if limit == "" {
			limit = " limit -1"
		}
		limit += " offset ?"
		args = append(args, query.Offset)
	}
	rows, err := s.db.QueryContext(ctx, `select id, email, password, status, workspaces, cookies, access_token,
		oauth_access_token, oauth_refresh_token, oauth_expires_at, oauth_plan_type, organization_id,
		last_mode, last_error, last_run_id, created_at, updated_at from accounts`+where+` order by id`+limit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (s *SQLiteStore) CountAccounts(ctx context.Context, query Query) (int, error) {
	where, args := accountWhere(query)
	var count int
	if err := s.db.QueryRowContext(ctx, `select count(*) from accounts`+where, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func accountWhere(query Query) (string, []any) {
	args := make([]any, 0, len(query.Statuses)+1)
	whereParts := []string{}
	statuses := normalizedStatuses(query.Statuses)
	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, status := range statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		whereParts = append(whereParts, "status in ("+strings.Join(placeholders, ",")+")")
	}
	if email := strings.ToLower(strings.TrimSpace(query.Email)); email != "" {
		whereParts = append(whereParts, "lower(email) like ?")
		args = append(args, "%"+email+"%")
	}
	if len(whereParts) == 0 {
		return "", args
	}
	return " where " + strings.Join(whereParts, " and "), args
}

func (s *SQLiteStore) UpdateFromResult(ctx context.Context, update ResultUpdate) error {
	email := strings.TrimSpace(update.Email)
	if email == "" {
		return errors.New("email is required")
	}
	status := statusFromResult(update)
	_, err := s.db.ExecContext(ctx, `update accounts set
		status = case
			when ? = '' then status
			when ? = 'oauth' and status = 'banned' then status
			else ?
		end,
		workspaces = coalesce(nullif(?, ''), workspaces),
		cookies = coalesce(nullif(?, ''), cookies),
		access_token = coalesce(nullif(?, ''), access_token),
		oauth_access_token = coalesce(nullif(?, ''), oauth_access_token),
		oauth_refresh_token = coalesce(nullif(?, ''), oauth_refresh_token),
		oauth_expires_at = coalesce(nullif(?, ''), oauth_expires_at),
		oauth_plan_type = coalesce(nullif(?, ''), oauth_plan_type),
		organization_id = coalesce(nullif(?, ''), organization_id),
		last_mode = ?,
		last_error = ?,
		last_run_id = ?,
		updated_at = ?
	where email = ?`,
		status, update.Mode, status, update.Workspaces, update.Cookies, update.AccessToken,
		update.OAuthAccessToken, update.OAuthRefreshToken, update.OAuthExpiresAt, update.OAuthPlanType,
		update.OrganizationID, update.Mode, update.ErrorMessage, update.RunID, formatTime(time.Now().UTC()), email)
	return err
}

func statusFromResult(update ResultUpdate) string {
	if update.Success {
		return "active"
	}
	msg := strings.ToLower(update.ErrorMessage)
	for _, marker := range []string{"banned", "ban", "disabled", "suspended", "封禁", "禁用"} {
		if strings.Contains(msg, marker) {
			return "banned"
		}
	}
	return ""
}

type accountScanner interface {
	Scan(dest ...any) error
}

func scanAccount(scanner accountScanner) (Account, error) {
	var account Account
	var workspaces, cookies, accessToken sql.NullString
	var oauthAccessToken, oauthRefreshToken, oauthExpiresAt, oauthPlanType sql.NullString
	var organizationID, lastMode, lastError, lastRunID sql.NullString
	var createdAt, updatedAt string
	if err := scanner.Scan(&account.ID, &account.Email, &account.Password, &account.Status,
		&workspaces, &cookies, &accessToken, &oauthAccessToken, &oauthRefreshToken, &oauthExpiresAt, &oauthPlanType,
		&organizationID, &lastMode, &lastError, &lastRunID, &createdAt, &updatedAt); err != nil {
		return Account{}, err
	}
	account.Workspaces = workspaces.String
	account.Cookies = cookies.String
	account.AccessToken = accessToken.String
	account.OAuthAccessToken = oauthAccessToken.String
	account.OAuthRefreshToken = oauthRefreshToken.String
	account.OAuthExpiresAt = oauthExpiresAt.String
	account.OAuthPlanType = oauthPlanType.String
	account.OrganizationID = organizationID.String
	account.LastMode = lastMode.String
	account.LastError = lastError.String
	account.LastRunID = lastRunID.String
	account.CreatedAt = parseTime(createdAt)
	account.UpdatedAt = parseTime(updatedAt)
	return account, nil
}

func (s *SQLiteStore) dropPhoneVerifiedColumnIfExists(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `select name from pragma_table_info('accounts')`)
	if err != nil {
		return err
	}
	exists := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "phone_verified" {
			exists = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `alter table accounts drop column phone_verified`)
	return err
}

func normalizedStatuses(statuses []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		status = strings.ToLower(strings.TrimSpace(status))
		if status == "" {
			continue
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out
}

func nullString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(sqliteTimeFormat)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{sqliteTimeFormat, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func sqliteDSN(path string, readonly bool) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	if readonly {
		values.Set("mode", "ro")
	}
	values.Add("_pragma", "foreign_keys(1)")
	values.Add("_pragma", "busy_timeout(5000)")
	return "file:" + abs + "?" + values.Encode(), nil
}

func requireMigrated(ctx context.Context, store *SQLiteStore) error {
	var count int
	if err := store.db.QueryRowContext(ctx, `select count(*) from sqlite_master where type='table' and name='accounts'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("database is not migrated; run -db-migrate first")
	}
	return nil
}
