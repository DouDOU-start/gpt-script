package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"chatgpt-register-script/internal/accountstore"
	"chatgpt-register-script/internal/config"
)

type webServerOptions struct {
	Addr           string
	ConfigPath     string
	YamlStore      *config.YamlStore
	YAMLConfig     config.YamlConfig
	Store          accountstore.Store
	OutputPath     string
	ScreenshotDir  string
	CodesPath      string
	ProxyURL       string
	PreferHostsRaw string
	IncludeSecrets bool
	DBWriteResults bool
}

type webServer struct {
	opts      config.YamlConfig
	yamlStore *config.YamlStore

	addr           string
	configPath     string
	store          accountstore.Store
	outputPath     string
	screenshotDir  string
	codesPath      string
	proxyURL       string
	preferHostsRaw string
	includeSecrets bool
	dbWriteResults bool
	logBuffer      *webLogBuffer

	mu   sync.Mutex
	jobs map[string]*webJob
}

type webLogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Line      string    `json:"line"`
}

type webLogBuffer struct {
	mu      sync.Mutex
	lines   []webLogLine
	pending string
	limit   int
}

type webJob struct {
	RunID       string    `json:"run_id"`
	RunDir      string    `json:"run_dir"`
	Mode        string    `json:"mode"`
	Status      string    `json:"status"`
	Accounts    int       `json:"accounts"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	cancel      context.CancelFunc
}

type accountResponse struct {
	ID                int64  `json:"id"`
	Email             string `json:"email"`
	Password          string `json:"password,omitempty"`
	Status            string `json:"status"`
	Workspaces        string `json:"workspaces,omitempty"`
	Cookies           string `json:"cookies,omitempty"`
	AccessToken       string `json:"access_token,omitempty"`
	OAuthAccessToken  string `json:"oauth_access_token,omitempty"`
	OAuthRefreshToken string `json:"oauth_refresh_token,omitempty"`
	OAuthExpiresAt    string `json:"oauth_expires_at,omitempty"`
	OAuthPlanType     string `json:"oauth_plan_type,omitempty"`
	OrganizationID    string `json:"organization_id,omitempty"`
	LastMode          string `json:"last_mode"`
	LastError         string `json:"last_error,omitempty"`
	LastRunID         string `json:"last_run_id,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

type runResponse struct {
	Job      *webJob        `json:"job,omitempty"`
	Manifest map[string]any `json:"manifest,omitempty"`
	Summary  map[string]any `json:"summary,omitempty"`
}

type webConfigResponse struct {
	ConfigPath      string             `json:"config_path"`
	BrowserBackend  string             `json:"browser_backend"`
	BrowserHeadless bool               `json:"browser_headless"`
	IncludeSecrets  bool               `json:"include_secrets"`
	RunsDir         string             `json:"runs_dir"`
	Proxy           config.ProxyConfig `json:"proxy"`
	Options         webConfigOptions   `json:"options"`
}

type webConfigOptions struct {
	BrowserBackends []string `json:"browser_backends"`
	ProxyModes      []string `json:"proxy_modes"`
}

type webConfigUpdateRequest struct {
	BrowserBackend  string   `json:"browser_backend"`
	BrowserHeadless bool     `json:"browser_headless"`
	IncludeSecrets  bool     `json:"include_secrets"`
	ProxyEnabled    bool     `json:"proxy_enabled"`
	ProxyMode       string   `json:"proxy_mode"`
	Proxy           string   `json:"proxy"`
	ProxyPoolFile   string   `json:"proxy_pool_file"`
	Proxies         []string `json:"proxies"`
}

func startWebServer(ctx context.Context, opts webServerOptions) error {
	if err := opts.Store.Migrate(ctx); err != nil {
		return fmt.Errorf("迁移账号数据库失败: %w", err)
	}
	if strings.TrimSpace(opts.Addr) == "" {
		opts.Addr = "127.0.0.1:8080"
	}
	logBuffer := &webLogBuffer{limit: 1000}
	s := &webServer{
		addr:           opts.Addr,
		configPath:     opts.ConfigPath,
		yamlStore:      opts.YamlStore,
		opts:           opts.YAMLConfig,
		store:          opts.Store,
		outputPath:     opts.OutputPath,
		screenshotDir:  opts.ScreenshotDir,
		codesPath:      opts.CodesPath,
		proxyURL:       opts.ProxyURL,
		preferHostsRaw: opts.PreferHostsRaw,
		includeSecrets: opts.IncludeSecrets,
		dbWriteResults: opts.DBWriteResults,
		logBuffer:      logBuffer,
		jobs:           map[string]*webJob{},
	}
	log.SetOutput(io.MultiWriter(os.Stderr, logBuffer))

	mux := http.NewServeMux()
	s.routes(mux)

	srv := &http.Server{Addr: opts.Addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("Web 管理页面: http://%s", opts.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *webServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/assets/", s.handleAsset)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/", s.handleAccount)
	mux.HandleFunc("/api/proxies", s.handleProxies)
	mux.HandleFunc("/api/proxies/", s.handleProxyRoute)
	mux.HandleFunc("/api/runs", s.handleRuns)
	mux.HandleFunc("/api/runs/", s.handleRun)
	mux.HandleFunc("/api/logs", s.handleLogs)
}

func (s *webServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimRight(r.URL.Path, "/") {
	case "", "/accounts", "/run", "/logs", "/proxy", "/config":
		http.ServeFileFS(w, r, webFiles, "web/dist/index.html")
	default:
		http.NotFound(w, r)
	}
}

func (s *webServer) handleAsset(w http.ResponseWriter, r *http.Request) {
	assetPath := strings.TrimPrefix(r.URL.Path, "/")
	http.ServeFileFS(w, r, webFiles, filepath.Join("web/dist", assetPath))
}

func (s *webServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.configResponse())
	case http.MethodPut:
		s.handleConfigUpdate(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *webServer) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req webConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	backend := strings.ToLower(strings.TrimSpace(req.BrowserBackend))
	if backend != "local" && backend != "docker" {
		writeError(w, http.StatusBadRequest, "browser_backend 只能是 local 或 docker")
		return
	}
	proxyMode := strings.ToLower(strings.TrimSpace(req.ProxyMode))
	if req.ProxyEnabled && proxyMode != "single" && proxyMode != "pool" {
		writeError(w, http.StatusBadRequest, "proxy_mode 只能是 single 或 pool")
		return
	}
	if proxyMode == "" {
		proxyMode = "single"
	}
	headless := req.BrowserHeadless
	includeSecrets := req.IncludeSecrets
	updated := s.opts
	updated.BrowserBackend = backend
	updated.BrowserHeadless = &headless
	updated.IncludeSecrets = &includeSecrets
	updated.Proxy.Enabled = req.ProxyEnabled
	updated.Proxy.Mode = proxyMode
	updated.Proxy.Proxy = strings.TrimSpace(req.Proxy)
	updated.Proxy.PoolFile = strings.TrimSpace(req.ProxyPoolFile)
	updated.Proxy.Proxies = trimStringSlice(req.Proxies)
	if !updated.Proxy.Enabled && updated.Proxy.Mode == "" {
		updated.Proxy.Mode = "single"
	}

	if s.yamlStore != nil {
		if err := s.yamlStore.Update(func(cfg *config.YamlConfig) error {
			cfg.BrowserBackend = updated.BrowserBackend
			cfg.BrowserHeadless = updated.BrowserHeadless
			cfg.IncludeSecrets = updated.IncludeSecrets
			cfg.Proxy.Enabled = updated.Proxy.Enabled
			cfg.Proxy.Mode = updated.Proxy.Mode
			cfg.Proxy.Proxy = updated.Proxy.Proxy
			cfg.Proxy.PoolFile = updated.Proxy.PoolFile
			cfg.Proxy.Proxies = append([]string(nil), updated.Proxy.Proxies...)
			return nil
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		updated = s.yamlStore.Get()
	}
	s.opts = updated
	s.includeSecrets = updated.IncludeSecretsValue()
	writeJSON(w, s.configResponse())
}

func (s *webServer) configResponse() webConfigResponse {
	cfg := s.opts
	return webConfigResponse{
		ConfigPath:      s.configPath,
		BrowserBackend:  cfg.BrowserBackend,
		BrowserHeadless: cfg.BrowserHeadlessValue(),
		IncludeSecrets:  cfg.IncludeSecretsValue(),
		RunsDir:         cfg.RunsDir,
		Proxy:           cfg.Proxy,
		Options: webConfigOptions{
			BrowserBackends: []string{"local", "docker"},
			ProxyModes:      []string{"single", "pool"},
		},
	}
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (s *webServer) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), parseIntDefault(r.URL.Query().Get("limit"), 100))
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}
	query := accountstore.Query{Statuses: splitCSV(r.URL.Query().Get("status")), Email: r.URL.Query().Get("email")}
	total, err := s.store.CountAccounts(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	query.Limit = pageSize
	query.Offset = (page - 1) * pageSize
	accounts, err := s.store.ListAccounts(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := make([]accountResponse, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, accountToResponse(account, false))
	}
	writeJSON(w, map[string]any{"accounts": rows, "page": page, "page_size": pageSize, "total": total})
}

func (s *webServer) handleAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := idFromPath(r.URL.Path, "/api/accounts/")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	account, ok, err := s.findAccountByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	writeJSON(w, accountToResponse(account, r.URL.Query().Get("secrets") == "1"))
}

func (s *webServer) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRunsList(w, r)
	case http.MethodPost:
		s.handleRunCreate(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *webServer) handleRunsList(w http.ResponseWriter, r *http.Request) {
	jobs := s.snapshotJobs()
	recent, err := listRecentRuns(s.opts.RunsDir, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"jobs": jobs, "recent": recent})
}

func (s *webServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 200)
	writeJSON(w, map[string]any{"logs": s.logBuffer.recent(limit)})
}

func (s *webServer) handleRunCreate(w http.ResponseWriter, r *http.Request) {
	var req webRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	job, err := s.startRun(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONStatus(w, http.StatusAccepted, job)
}

func (s *webServer) handleRun(w http.ResponseWriter, r *http.Request) {
	runID, action, ok := runRoute(r.URL.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "missing run id")
		return
	}
	s.dispatchRunRoute(w, r, runID, action)
}

func runRoute(path string) (runID string, action string, ok bool) {
	trimmed := strings.TrimPrefix(path, "/api/runs/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	if len(parts) > 1 {
		action = parts[1]
	}
	return parts[0], action, true
}

func (s *webServer) dispatchRunRoute(w http.ResponseWriter, r *http.Request, runID, action string) {
	switch action {
	case "":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleRunGet(w, r, runID)
	case "events":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleRunEvents(w, r, runID)
	case "stop":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleRunStop(w, r, runID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *webServer) handleRunGet(w http.ResponseWriter, r *http.Request, runID string) {
	runDir := filepath.Join(s.opts.RunsDir, runID)
	manifest, _ := readJSONMap(filepath.Join(runDir, "manifest.json"))
	summary, _ := readJSONMap(filepath.Join(runDir, "summary.json"))
	job := s.job(runID)
	if job == nil && manifest == nil && summary == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, runResponse{Job: job, Manifest: manifest, Summary: summary})
}

func (s *webServer) handleRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
	limit := parseIntDefault(r.URL.Query().Get("limit"), 200)
	events, err := readRecentJSONL(filepath.Join(s.opts.RunsDir, runID, "events.jsonl"), limit)
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"events": events})
}

func (s *webServer) handleRunStop(w http.ResponseWriter, r *http.Request, runID string) {
	s.mu.Lock()
	job := s.jobs[runID]
	if job != nil && job.Status == "running" {
		job.Status = "stopping"
		job.cancel()
	}
	s.mu.Unlock()
	if job == nil {
		writeError(w, http.StatusNotFound, "active run not found")
		return
	}
	writeJSON(w, job)
}

func (s *webServer) findAccountByID(ctx context.Context, id int64) (accountstore.Account, bool, error) {
	accounts, err := s.store.ListAccounts(ctx, accountstore.Query{})
	if err != nil {
		return accountstore.Account{}, false, err
	}
	for _, account := range accounts {
		if account.ID == id {
			return account, true, nil
		}
	}
	return accountstore.Account{}, false, nil
}

func (s *webServer) snapshotJobs() []*webJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]*webJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		copy := *job
		copy.cancel = nil
		jobs = append(jobs, &copy)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].StartedAt.After(jobs[j].StartedAt) })
	return jobs
}

func (s *webServer) job(runID string) *webJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[runID]
	if job == nil {
		return nil
	}
	copy := *job
	copy.cancel = nil
	return &copy
}

func (s *webServer) updateJob(runID, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[runID]
	if job == nil {
		return
	}
	job.Status = status
	if message != "" {
		job.Error = message
	}
	if status == "completed" || status == "failed" || status == "stopped" {
		job.CompletedAt = time.Now().UTC()
	}
}

func accountToResponse(account accountstore.Account, includeSecrets bool) accountResponse {
	resp := accountResponse{
		ID:             account.ID,
		Email:          account.Email,
		Status:         account.Status,
		OAuthExpiresAt: account.OAuthExpiresAt,
		OAuthPlanType:  account.OAuthPlanType,
		OrganizationID: account.OrganizationID,
		LastMode:       account.LastMode,
		LastError:      account.LastError,
		LastRunID:      account.LastRunID,
		CreatedAt:      formatTime(account.CreatedAt),
		UpdatedAt:      formatTime(account.UpdatedAt),
	}
	if includeSecrets {
		resp.Password = account.Password
		resp.Workspaces = account.Workspaces
		resp.Cookies = account.Cookies
		resp.AccessToken = account.AccessToken
		resp.OAuthAccessToken = account.OAuthAccessToken
		resp.OAuthRefreshToken = account.OAuthRefreshToken
	}
	return resp
}

func (b *webLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending += string(p)
	for {
		index := strings.IndexByte(b.pending, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimRight(b.pending[:index], "\r")
		b.pending = b.pending[index+1:]
		if line == "" {
			continue
		}
		b.lines = append(b.lines, webLogLine{Timestamp: time.Now().UTC(), Line: line})
		if b.limit > 0 && len(b.lines) > b.limit {
			b.lines = b.lines[len(b.lines)-b.limit:]
		}
	}
	return len(p), nil
}

func (b *webLogBuffer) recent(limit int) []webLogLine {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := b.lines
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]webLogLine, len(lines))
	copy(out, lines)
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func idFromPath(path, prefix string) (int64, error) {
	raw := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func parseIntDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func readJSONMap(path string) (map[string]any, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func readRecentJSONL(path string, limit int) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err == nil {
			rows = append(rows, row)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

func listRecentRuns(runsDir string, limit int) ([]map[string]any, error) {
	entries, err := os.ReadDir(firstNonEmpty(runsDir, "runs"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var runs []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		runs = append(runs, map[string]any{"run_id": entry.Name(), "updated_at": info.ModTime().Format(time.RFC3339)})
	}
	sort.Slice(runs, func(i, j int) bool {
		return fmt.Sprint(runs[i]["run_id"]) > fmt.Sprint(runs[j]["run_id"])
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]any{"error": message})
}
