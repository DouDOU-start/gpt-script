package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateProxy(ctx context.Context, proxy Proxy) (Proxy, error) {
	proxy.Host = strings.TrimSpace(proxy.Host)
	proxy.Protocol = normalizedProxyProtocol(proxy.Protocol)
	if proxy.Host == "" || proxy.Port <= 0 || proxy.Port > 65535 {
		return Proxy{}, errors.New("host and valid port are required")
	}
	if proxy.Status == "" {
		proxy.Status = "active"
	}
	if proxy.CreatedAt.IsZero() {
		proxy.CreatedAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `insert into proxies (
		host, port, protocol, username, password, enabled, status, fail_count, last_used_at, last_checked_at, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		proxy.Host, proxy.Port, proxy.Protocol, nullString(proxy.Username), nullString(proxy.Password), boolInt(proxy.Enabled), proxy.Status,
		proxy.FailCount, nullTime(proxy.LastUsedAt), nullTime(proxy.LastCheckedAt), formatTime(proxy.CreatedAt))
	if err != nil {
		return Proxy{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Proxy{}, err
	}
	proxy.ID = id
	return proxy, nil
}

func (s *SQLiteStore) ListProxies(ctx context.Context, query ProxyQuery) ([]Proxy, error) {
	where, args := proxyWhere(query)
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
	rows, err := s.db.QueryContext(ctx, `select id, host, port, protocol, username, password, enabled, status,
		fail_count, last_used_at, last_checked_at, created_at from proxies`+where+` order by created_at desc, id desc`+limit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	proxies := []Proxy{}
	for rows.Next() {
		proxy, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, proxy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return proxies, nil
}

func (s *SQLiteStore) CountProxies(ctx context.Context, query ProxyQuery) (int, error) {
	where, args := proxyWhere(query)
	var count int
	if err := s.db.QueryRowContext(ctx, `select count(*) from proxies`+where, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) GetProxy(ctx context.Context, id int64) (Proxy, bool, error) {
	proxy, err := scanProxy(s.db.QueryRowContext(ctx, `select id, host, port, protocol, username, password, enabled, status,
		fail_count, last_used_at, last_checked_at, created_at from proxies where id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Proxy{}, false, nil
	}
	if err != nil {
		return Proxy{}, false, err
	}
	return proxy, true, nil
}

func (s *SQLiteStore) DeleteProxy(ctx context.Context, id int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `delete from proxies where id = ?`, id)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func (s *SQLiteStore) SetProxyEnabled(ctx context.Context, id int64, enabled bool) (bool, error) {
	result, err := s.db.ExecContext(ctx, `update proxies set enabled = ? where id = ?`, boolInt(enabled), id)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func (s *SQLiteStore) BatchDeleteProxies(ctx context.Context, ids []int64) (int64, error) {
	query, args := idInQuery(`delete from proxies where id in (`, ids)
	if query == "" {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteStore) BatchSetProxyEnabled(ctx context.Context, ids []int64, enabled bool) (int64, error) {
	query, args := idInQuery(`update proxies set enabled = ? where id in (`, ids)
	if query == "" {
		return 0, nil
	}
	args = append([]any{boolInt(enabled)}, args...)
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteStore) UpdateProxyCheck(ctx context.Context, id int64, success bool, checkedAt time.Time) error {
	status := "active"
	if success {
		_, err := s.db.ExecContext(ctx, `update proxies set status = ?, fail_count = 0, last_checked_at = ? where id = ?`, status, formatTime(checkedAt), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `update proxies set
		fail_count = fail_count + 1,
		status = case when fail_count + 1 >= 3 then 'inactive' else status end,
		last_checked_at = ?
	where id = ?`, formatTime(checkedAt), id)
	return err
}

func proxyWhere(query ProxyQuery) (string, []any) {
	status := strings.ToLower(strings.TrimSpace(query.Status))
	if status == "" || status == "all" {
		return "", nil
	}
	return " where status = ?", []any{status}
}

type proxyScanner interface {
	Scan(dest ...any) error
}

func scanProxy(scanner proxyScanner) (Proxy, error) {
	var proxy Proxy
	var username, password, lastUsedAt, lastCheckedAt sql.NullString
	var enabled int
	var createdAt string
	if err := scanner.Scan(&proxy.ID, &proxy.Host, &proxy.Port, &proxy.Protocol, &username, &password, &enabled,
		&proxy.Status, &proxy.FailCount, &lastUsedAt, &lastCheckedAt, &createdAt); err != nil {
		return Proxy{}, err
	}
	proxy.Username = username.String
	proxy.Password = password.String
	proxy.Enabled = enabled != 0
	proxy.LastUsedAt = parseTime(lastUsedAt.String)
	proxy.LastCheckedAt = parseTime(lastCheckedAt.String)
	proxy.CreatedAt = parseTime(createdAt)
	return proxy, nil
}

func normalizedProxyProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		return "http"
	}
	return protocol
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullTime(value time.Time) sql.NullString {
	if value.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(value), Valid: true}
}

func idInQuery(prefix string, ids []int64) (string, []any) {
	seen := map[int64]struct{}{}
	placeholders := []string{}
	args := []any{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(placeholders) == 0 {
		return "", nil
	}
	return prefix + strings.Join(placeholders, ",") + `)`, args
}
