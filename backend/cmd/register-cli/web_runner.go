package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chatgpt-register-script/internal/accountstore"
	"chatgpt-register-script/internal/dockerpool"
	migratedruntime "chatgpt-register-script/internal/runtime"
)

type webRunRequest struct {
	Mode           string  `json:"mode"`
	Workers        int     `json:"workers"`
	Backend        string  `json:"backend"`
	Headless       *bool   `json:"headless"`
	IncludeSecrets *bool   `json:"include_secrets"`
	ProxyEnabled   *bool   `json:"proxy_enabled"`
	ProxyMode      string  `json:"proxy_mode"`
	Proxy          string  `json:"proxy"`
	ProxyPoolFile  string  `json:"proxy_pool_file"`
	WorkspaceID    string  `json:"workspace_id"`
	OrganizationID string  `json:"organization_id"`
	AccountIDs     []int64 `json:"account_ids"`
	Status         string  `json:"status"`
	Limit          int     `json:"limit"`
}

func (s *webServer) startRun(parent context.Context, req webRunRequest) (*webJob, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "oauth"
	}
	if mode != "register" && mode != "login" && mode != "oauth" {
		return nil, fmt.Errorf("mode 只能是 register、login 或 oauth")
	}
	workers := req.Workers
	if workers <= 0 {
		workers = 1
	}
	accounts, err := s.accountsForRun(parent, mode, req)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("没有匹配账号")
	}
	for i := range accounts {
		accounts[i].Index = i
	}

	yamlCfg := s.opts
	if strings.TrimSpace(req.Backend) != "" {
		yamlCfg.BrowserBackend = strings.TrimSpace(req.Backend)
	}
	if req.Headless != nil {
		yamlCfg.BrowserHeadless = req.Headless
	}
	proxyURL := strings.TrimSpace(req.Proxy)
	proxyMode := strings.ToLower(strings.TrimSpace(req.ProxyMode))
	proxyPoolFile := strings.TrimSpace(req.ProxyPoolFile)
	if req.ProxyEnabled != nil && !*req.ProxyEnabled {
		yamlCfg.Proxy.Enabled = false
		proxyURL = ""
	} else if proxyMode == "pool" || proxyPoolFile != "" {
		yamlCfg.Proxy.Enabled = true
		yamlCfg.Proxy.Mode = "pool"
		yamlCfg.Proxy.PoolFile = proxyPoolFile
		yamlCfg.Proxy.Proxy = ""
		proxyURL = ""
	} else {
		proxyURL = firstNonEmpty(proxyURL, s.proxyURL)
		if strings.TrimSpace(proxyURL) != "" {
			yamlCfg.Proxy.Enabled = true
			yamlCfg.Proxy.Mode = "single"
			yamlCfg.Proxy.Proxy = strings.TrimSpace(proxyURL)
		}
	}

	if err := os.MkdirAll(filepath.Dir(s.outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建结果目录失败: %w", err)
	}
	if mode != "oauth" {
		if err := os.MkdirAll(s.screenshotDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建截图目录失败: %w", err)
		}
	}
	out, err := os.OpenFile(s.outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开结果文件失败: %w", err)
	}

	planned := make([]migratedruntime.PlannedAccount, 0, len(accounts))
	for _, acc := range accounts {
		planned = append(planned, migratedruntime.PlannedAccount{Index: acc.Index, Email: acc.Email, Password: acc.Password, Name: "ChatGPT User"})
	}
	runCtx, err := migratedruntime.CreatePlannedRun(mode, planned, yamlCfg.RunsDir, map[string]any{"proxy_enabled": yamlCfg.Proxy.Enabled, "source": "web"})
	if err != nil {
		_ = out.Close()
		return nil, fmt.Errorf("创建运行目录失败: %w", err)
	}
	runStore := migratedruntime.StoreFromContext(*runCtx)
	runCtxBase, cancel := context.WithCancel(context.Background())
	includeSecrets := yamlCfg.IncludeSecretsValue()
	if req.IncludeSecrets != nil {
		includeSecrets = *req.IncludeSecrets
	}
	job := &webJob{
		RunID:     runCtx.RunID,
		RunDir:    runCtx.RunDir,
		Mode:      mode,
		Status:    "running",
		Accounts:  len(accounts),
		StartedAt: time.Now().UTC(),
		cancel:    cancel,
	}
	s.mu.Lock()
	s.jobs[job.RunID] = job
	s.mu.Unlock()

	go func() {
		defer out.Close()
		var dockerPool *dockerpool.Pool
		if mode != "oauth" && strings.TrimSpace(yamlCfg.BrowserBackend) == "docker" {
			dockerPool = dockerpool.New()
			dockerPool.Initialize(runCtxBase, yamlCfg)
		}
		rc := runnerConfig{
			mode:           mode,
			proxyURL:       proxyURL,
			screenshotDir:  s.screenshotDir,
			includeSecrets: includeSecrets,
			interactive:    false,
			codesPath:      s.codesPath,
			stdin:          bufio.NewReader(strings.NewReader("")),
			preferHosts:    splitCSV(s.preferHostsRaw),
			workspaceID:    strings.TrimSpace(req.WorkspaceID),
			organizationID: strings.TrimSpace(req.OrganizationID),
			runsDir:        yamlCfg.RunsDir,
			runID:          runCtx.RunID,
			runStore:       &runStore,
			accountStore:   s.store,
			dbWriteResults: s.dbWriteResults,
			proxyPool:      buildProxyPool(yamlCfg, s.configPath),
		}
		runBatch(runCtxBase, accounts, workers, dockerPool, yamlCfg, &rc, out)
		if runCtxBase.Err() != nil {
			s.updateJob(runCtx.RunID, "stopped", runCtxBase.Err().Error())
			return
		}
		s.updateJob(runCtx.RunID, "completed", "")
	}()

	copy := *job
	copy.cancel = nil
	return &copy, nil
}

func (s *webServer) accountsForRun(ctx context.Context, mode string, req webRunRequest) ([]accountInput, error) {
	statuses := splitCSV(req.Status)
	if len(statuses) == 0 {
		statuses = dbStatusesForMode(mode, "")
	}
	limit := req.Limit
	if limit <= 0 && len(req.AccountIDs) == 0 {
		limit = 50
	}
	queryLimit := limit
	if len(req.AccountIDs) > 0 {
		queryLimit = 0
		statuses = nil
	}
	accounts, err := s.store.ListAccounts(ctx, accountstore.Query{Statuses: statuses, Limit: queryLimit})
	if err != nil {
		return nil, err
	}
	wanted := map[int64]bool{}
	for _, id := range req.AccountIDs {
		wanted[id] = true
	}
	inputs := make([]accountInput, 0, len(accounts))
	for _, account := range accounts {
		if len(wanted) > 0 && !wanted[account.ID] {
			continue
		}
		inputs = append(inputs, accountInput{Email: account.Email, Password: account.Password, DBID: account.ID, Status: account.Status})
		if limit > 0 && len(inputs) >= limit {
			break
		}
	}
	return inputs, nil
}
