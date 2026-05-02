package accountstore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (s *SQLiteStore) ImportLegacy(ctx context.Context, sourcePath string) (ImportSummary, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return ImportSummary{}, fmt.Errorf("legacy database path is required")
	}
	if err := s.Migrate(ctx); err != nil {
		return ImportSummary{}, err
	}
	dsn, err := sqliteDSN(sourcePath, true)
	if err != nil {
		return ImportSummary{}, err
	}
	source, err := sql.Open("sqlite", dsn)
	if err != nil {
		return ImportSummary{}, err
	}
	defer source.Close()
	if err := source.PingContext(ctx); err != nil {
		return ImportSummary{}, err
	}

	rows, err := source.QueryContext(ctx, `select email, password, status, workspaces, cookies, oauth, created_at, updated_at from accounts order by id`)
	if err != nil {
		return ImportSummary{}, err
	}
	defer rows.Close()

	summary := ImportSummary{}
	for rows.Next() {
		var email, password string
		var status, workspaces, cookies, oauth sql.NullString
		var createdRaw, updatedRaw sql.NullString
		if err := rows.Scan(&email, &password, &status, &workspaces, &cookies, &oauth, &createdRaw, &updatedRaw); err != nil {
			return summary, err
		}
		email = strings.TrimSpace(email)
		if email == "" || password == "" {
			summary.Invalid++
			continue
		}
		account := Account{
			Email:      email,
			Password:   password,
			Status:     strings.ToLower(strings.TrimSpace(status.String)),
			Workspaces: workspaces.String,
			Cookies:    cookies.String,
			CreatedAt:  parseLegacyTime(createdRaw.String),
			UpdatedAt:  parseLegacyTime(updatedRaw.String),
		}
		if account.Status == "" {
			account.Status = "active"
		}
		if looksLikeToken(oauth.String) {
			account.OAuthAccessToken = strings.TrimSpace(oauth.String)
		}
		created, err := s.accountExists(ctx, account.Email)
		if err != nil {
			return summary, err
		}
		if _, err := s.UpsertAccount(ctx, account); err != nil {
			summary.Invalid++
			continue
		}
		if created {
			summary.Updated++
		} else {
			summary.Imported++
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

func (s *SQLiteStore) accountExists(ctx context.Context, email string) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `select 1 from accounts where email = ?`, email).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func parseLegacyTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed := parseTime(value); !parsed.IsZero() {
		return parsed
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}

func looksLikeToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return false
	}
	return len(value) >= 20 && !strings.ContainsAny(value, " \n\t")
}
