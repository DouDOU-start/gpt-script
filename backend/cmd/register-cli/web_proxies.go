package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"chatgpt-register-script/internal/accountstore"
)

type proxyResponse struct {
	ID            int64   `json:"id"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Protocol      string  `json:"protocol"`
	Enabled       bool    `json:"enabled"`
	Status        string  `json:"status"`
	FailCount     int     `json:"fail_count"`
	LastUsedAt    *string `json:"last_used_at"`
	LastCheckedAt *string `json:"last_checked_at"`
	CreatedAt     string  `json:"created_at"`
}

func (s *webServer) handleProxies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleProxiesList(w, r)
	case http.MethodPost:
		s.handleProxyCreate(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *webServer) handleProxyRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/proxies/"), "/")
	switch trimmed {
	case "import":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleProxiesImport(w, r)
		return
	case "batch-delete":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleProxiesBatchDelete(w, r)
		return
	case "batch-toggle":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleProxiesBatchToggle(w, r)
		return
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid proxy id")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		s.handleProxyDelete(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost {
		s.handleProxyTest(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "enabled" && r.Method == http.MethodPut {
		s.handleProxyEnabled(w, r, id)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *webServer) handleProxiesList(w http.ResponseWriter, r *http.Request) {
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	query := accountstore.ProxyQuery{Status: r.URL.Query().Get("status")}
	total, err := s.store.CountProxies(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	query.Limit = pageSize
	query.Offset = (page - 1) * pageSize
	rows, err := s.store.ListProxies(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]proxyResponse, 0, len(rows))
	for _, proxy := range rows {
		items = append(items, proxyToResponse(proxy))
	}
	writeJSON(w, map[string]any{"items": items, "page": page, "page_size": pageSize, "total": total})
}

func (s *webServer) handleProxyCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	proxy, err := normalizeProxy(accountstore.Proxy{Host: req.Host, Port: req.Port, Protocol: req.Protocol, Username: req.Username, Password: req.Password, Enabled: true, Status: "active"})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreateProxy(r.Context(), proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, proxyToResponse(created))
}

func (s *webServer) handleProxiesImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Proxies []string `json:"proxies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	successCount := 0
	failedCount := 0
	errors := []string{}
	for _, line := range req.Proxies {
		proxy, err := parseProxyLine(line)
		if err != nil {
			failedCount++
			errors = append(errors, fmt.Sprintf("%s: %s", strings.TrimSpace(line), err.Error()))
			continue
		}
		if _, err := s.store.CreateProxy(r.Context(), proxy); err != nil {
			failedCount++
			errors = append(errors, fmt.Sprintf("%s: %s", strings.TrimSpace(line), err.Error()))
			continue
		}
		successCount++
	}
	writeJSON(w, map[string]any{"success_count": successCount, "failed_count": failedCount, "errors": errors})
}

func (s *webServer) handleProxyDelete(w http.ResponseWriter, r *http.Request, id int64) {
	deleted, err := s.store.DeleteProxy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}
	writeJSON(w, map[string]any{"deleted": true})
}

func (s *webServer) handleProxyEnabled(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	updated, err := s.store.SetProxyEnabled(r.Context(), id, req.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !updated {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}
	writeJSON(w, map[string]any{"enabled": req.Enabled})
}

func (s *webServer) handleProxiesBatchDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	count, err := s.store.BatchDeleteProxies(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"deleted_count": count})
}

func (s *webServer) handleProxiesBatchToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs     []int64 `json:"ids"`
		Enabled bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	count, err := s.store.BatchSetProxyEnabled(r.Context(), req.IDs, req.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"updated_count": count})
}

func (s *webServer) handleProxyTest(w http.ResponseWriter, r *http.Request, id int64) {
	proxy, ok, err := s.store.GetProxy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}

	start := time.Now().UTC()
	proxyURL, err := proxyURLFromParts(proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(parsed)},
	}
	resp, err := client.Get("https://api.ipify.org?format=json")
	checkedAt := time.Now().UTC()
	if err != nil {
		_ = s.store.UpdateProxyCheck(r.Context(), id, false, checkedAt)
		writeJSON(w, map[string]any{"proxy_id": id, "success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = s.store.UpdateProxyCheck(r.Context(), id, false, checkedAt)
		writeJSON(w, map[string]any{"proxy_id": id, "success": false, "error": fmt.Sprintf("http=%d %s", resp.StatusCode, string(body))})
		return
	}
	var ipResp struct {
		IP string `json:"ip"`
	}
	_ = json.Unmarshal(body, &ipResp)
	_ = s.store.UpdateProxyCheck(r.Context(), id, true, checkedAt)
	writeJSON(w, map[string]any{"proxy_id": id, "success": true, "response_time_ms": int(time.Since(start).Milliseconds()), "ip": ipResp.IP})
}

func proxyToResponse(proxy accountstore.Proxy) proxyResponse {
	return proxyResponse{
		ID:            proxy.ID,
		Host:          proxy.Host,
		Port:          proxy.Port,
		Protocol:      proxy.Protocol,
		Enabled:       proxy.Enabled,
		Status:        proxy.Status,
		FailCount:     proxy.FailCount,
		LastUsedAt:    optionalTime(proxy.LastUsedAt),
		LastCheckedAt: optionalTime(proxy.LastCheckedAt),
		CreatedAt:     formatTime(proxy.CreatedAt),
	}
}

func optionalTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func normalizeProxy(proxy accountstore.Proxy) (accountstore.Proxy, error) {
	proxy.Host = strings.TrimSpace(proxy.Host)
	proxy.Username = strings.TrimSpace(proxy.Username)
	proxy.Password = strings.TrimSpace(proxy.Password)
	proxy.Protocol = strings.ToLower(strings.TrimSpace(proxy.Protocol))
	if proxy.Protocol == "" {
		proxy.Protocol = "http"
	}
	if proxy.Protocol != "http" && proxy.Protocol != "https" && proxy.Protocol != "socks5" {
		return accountstore.Proxy{}, fmt.Errorf("unsupported protocol: %s", proxy.Protocol)
	}
	if proxy.Host == "" || proxy.Port <= 0 || proxy.Port > 65535 {
		return accountstore.Proxy{}, fmt.Errorf("host and valid port are required")
	}
	if proxy.Status == "" {
		proxy.Status = "active"
	}
	return proxy, nil
}

func parseProxyLine(raw string) (accountstore.Proxy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return accountstore.Proxy{}, fmt.Errorf("empty proxy")
	}
	if matches := regexp.MustCompile(`^([^:]+):(\d+):([^:]+):(.+)$`).FindStringSubmatch(raw); matches != nil {
		port, _ := strconv.Atoi(matches[2])
		return normalizeProxy(accountstore.Proxy{Host: matches[1], Port: port, Protocol: "http", Username: matches[3], Password: matches[4], Enabled: true, Status: "active"})
	}
	if matches := regexp.MustCompile(`^([^:]+):(\d+)$`).FindStringSubmatch(raw); matches != nil {
		port, _ := strconv.Atoi(matches[2])
		return normalizeProxy(accountstore.Proxy{Host: matches[1], Port: port, Protocol: "http", Enabled: true, Status: "active"})
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return accountstore.Proxy{}, fmt.Errorf("invalid proxy format")
	}
	host, portValue, err := net.SplitHostPort(u.Host)
	if err != nil {
		return accountstore.Proxy{}, fmt.Errorf("proxy must include host and port")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return accountstore.Proxy{}, fmt.Errorf("invalid port")
	}
	proxy := accountstore.Proxy{Host: host, Port: port, Protocol: u.Scheme, Enabled: true, Status: "active"}
	if u.User != nil {
		proxy.Username = u.User.Username()
		proxy.Password, _ = u.User.Password()
	}
	return normalizeProxy(proxy)
}

func proxyURLFromParts(proxy accountstore.Proxy) (string, error) {
	proxy, err := normalizeProxy(proxy)
	if err != nil {
		return "", err
	}
	u := url.URL{Scheme: proxy.Protocol, Host: net.JoinHostPort(proxy.Host, strconv.Itoa(proxy.Port))}
	if proxy.Username != "" || proxy.Password != "" {
		u.User = url.UserPassword(proxy.Username, proxy.Password)
	}
	return u.String(), nil
}
