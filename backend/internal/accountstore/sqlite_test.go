package accountstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStoreMigrateUpsertListAndUpdate(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if _, err := store.UpsertAccount(ctx, Account{Email: "active@example.com", Password: "secret", Status: "active"}); err != nil {
		t.Fatalf("UpsertAccount(active) error = %v", err)
	}
	if _, err := store.UpsertAccount(ctx, Account{Email: "banned@example.com", Password: "secret", Status: "banned"}); err != nil {
		t.Fatalf("UpsertAccount(banned) error = %v", err)
	}

	accounts, err := store.ListAccounts(ctx, Query{Statuses: []string{"active"}, Limit: 10})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].Email != "active@example.com" {
		t.Fatalf("ListAccounts() = %#v", accounts)
	}
	accounts, err = store.ListAccounts(ctx, Query{Email: "BANNED@EXAMPLE"})
	if err != nil {
		t.Fatalf("ListAccounts(email) error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].Email != "banned@example.com" {
		t.Fatalf("ListAccounts(email) = %#v", accounts)
	}
	count, err := store.CountAccounts(ctx, Query{})
	if err != nil {
		t.Fatalf("CountAccounts() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("CountAccounts() = %d, want 2", count)
	}
	accounts, err = store.ListAccounts(ctx, Query{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListAccounts(offset) error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].Email != "banned@example.com" {
		t.Fatalf("ListAccounts(offset) = %#v", accounts)
	}

	err = store.UpdateFromResult(ctx, ResultUpdate{
		Email:             "active@example.com",
		Mode:              "oauth",
		Success:           true,
		RunID:             "run_1",
		OAuthAccessToken:  "access-token",
		OAuthRefreshToken: "refresh-token",
		OAuthExpiresAt:    time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("UpdateFromResult() error = %v", err)
	}
	accounts, err = store.ListAccounts(ctx, Query{Statuses: []string{"active"}})
	if err != nil {
		t.Fatalf("ListAccounts() after update error = %v", err)
	}
	if accounts[0].OAuthAccessToken != "access-token" || accounts[0].LastRunID != "run_1" || accounts[0].LastMode != "oauth" {
		t.Fatalf("updated account = %#v", accounts[0])
	}

	if err := store.UpdateFromResult(ctx, ResultUpdate{Email: "active@example.com", Mode: "login", ErrorMessage: "account banned"}); err != nil {
		t.Fatalf("UpdateFromResult(banned) error = %v", err)
	}
	accounts, err = store.ListAccounts(ctx, Query{Statuses: []string{"banned"}})
	if err != nil {
		t.Fatalf("ListAccounts(banned) error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("banned count = %d, want 2", len(accounts))
	}
}

func TestUpdateFromResultKeepsBannedStatusOnOAuthSuccess(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := store.UpsertAccount(ctx, Account{Email: "banned@example.com", Password: "secret", Status: "banned"}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	if err := store.UpdateFromResult(ctx, ResultUpdate{Email: "banned@example.com", Mode: "oauth", Success: true, OAuthAccessToken: "token"}); err != nil {
		t.Fatalf("UpdateFromResult() error = %v", err)
	}
	accounts, err := store.ListAccounts(ctx, Query{Statuses: []string{"banned"}})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].OAuthAccessToken != "token" {
		t.Fatalf("accounts = %#v", accounts)
	}
}

func TestImportLegacy(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = legacy.Exec(`create table accounts(
		id int,
		email text,
		password text,
		status text,
		phone_verified num,
		workspaces text,
		cookies text,
		oauth text,
		created_at num,
		updated_at num
	)`)
	if err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	_, err = legacy.Exec(`insert into accounts values
		(1, 'active@example.com', 'pw1', 'active', 1, '[{}]', 'cookies', 'tok_12345678901234567890', 1700000000, 1700000001),
		(2, 'banned@example.com', 'pw2', 'banned', 0, '', '', '', 1700000000, 1700000001),
		(3, '', 'missing-email', 'active', 0, '', '', '', 1700000000, 1700000001)
	`)
	if err != nil {
		t.Fatalf("insert legacy rows: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	summary, err := store.ImportLegacy(ctx, legacyPath)
	if err != nil {
		t.Fatalf("ImportLegacy() error = %v", err)
	}
	if summary.Imported != 2 || summary.Invalid != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	accounts, err := store.ListAccounts(ctx, Query{})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
	if accounts[0].Email != "active@example.com" || accounts[0].Password != "pw1" || accounts[0].Status != "active" {
		t.Fatalf("first imported account = %#v", accounts[0])
	}
	if accounts[0].OAuthAccessToken == "" {
		t.Fatalf("oauth token was not preserved: %#v", accounts[0])
	}
}

func TestMigrateDropsPhoneVerifiedColumn(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.ExecContext(ctx, `create table accounts(
		id integer primary key autoincrement,
		email text not null unique,
		password text not null,
		status text not null default 'active',
		phone_verified boolean not null default false,
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
	)`)
	if err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	_, err = store.db.ExecContext(ctx, `insert into accounts(email, password, status, phone_verified, workspaces, created_at, updated_at) values
		('active@example.com', 'secret', 'active', 1, '[{}]', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if columnExists(t, store, "accounts", "phone_verified") {
		t.Fatalf("phone_verified column still exists")
	}
	accounts, err := store.ListAccounts(ctx, Query{})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].Email != "active@example.com" || accounts[0].Workspaces != "[{}]" {
		t.Fatalf("accounts after migration = %#v", accounts)
	}
}

func columnExists(t *testing.T, store *SQLiteStore, table, column string) bool {
	t.Helper()
	rows, err := store.db.Query(`select name from pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	return false
}

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	return store
}
