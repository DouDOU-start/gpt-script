package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"chatgpt-register-script/internal/config"
	"chatgpt-register-script/internal/dockerpool"
	"chatgpt-register-script/internal/oauth"
	"chatgpt-register-script/internal/protocolregister"
	migratedproxy "chatgpt-register-script/internal/proxy"
	"chatgpt-register-script/internal/register"
)

func runBatch(ctx context.Context, accounts []accountInput, workers int, dockerPool *dockerpool.Pool, yamlCfg config.YamlConfig, rc *runnerConfig, out *os.File) {
	jobs := make(chan accountInput)
	var wg sync.WaitGroup
	var outMu sync.Mutex

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for acc := range jobs {
				if rc.runStore != nil {
					rc.runStore.AppendStageEvent(acc.Index, acc.Email, rc.mode, "started", "", map[string]any{"worker_id": workerID})
				}
				result := runOne(ctx, acc, dockerPool, yamlCfg, rc)
				outMu.Lock()
				_ = json.NewEncoder(out).Encode(result)
				_ = out.Sync()
				outMu.Unlock()

				if rc.runStore != nil {
					status := "failed"
					if result.Success {
						status = "succeeded"
					}
					rc.runStore.AppendStageEvent(acc.Index, acc.Email, rc.mode, status, result.ErrorMessage, map[string]any{"task_id": result.TaskID})
					_ = rc.runStore.UpdateAccount(acc.Index, map[string]any{rc.mode: map[string]any{"status": status, "result": result}})
					if result.Success {
						_ = rc.runStore.IncrementSummary(summaryFieldForMode(rc.mode), 1)
					} else {
						_ = rc.runStore.IncrementSummary("failed", 1)
					}
				}
				if result.Success {
					log.Printf("[worker-%d] %s 成功", workerID, acc.Email)
				} else {
					log.Printf("[worker-%d] %s 失败: %s", workerID, acc.Email, result.ErrorMessage)
				}
			}
		}(i + 1)
	}

sendLoop:
	for _, acc := range accounts {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- acc:
		}
	}
	close(jobs)
	wg.Wait()
}

func runOne(ctx context.Context, acc accountInput, dockerPool *dockerpool.Pool, yamlCfg config.YamlConfig, rc *runnerConfig) runResult {
	startedAt := time.Now().UTC()
	taskID := newTaskID()
	result := runResult{
		TaskID:    taskID,
		Email:     acc.Email,
		Mode:      rc.mode,
		StartedAt: startedAt.Format(time.RFC3339),
	}
	if rc.includeSecrets {
		result.Password = acc.Password
	}
	stage := migratedproxy.StageRegister
	if rc.mode == "oauth" {
		stage = migratedproxy.StageOAuth
	}
	proxyURL := rc.proxyURL
	if rc.proxyPool != nil {
		if selected := rc.proxyPool.ProxyFor(stage, acc.Index); selected != "" {
			proxyURL = selected
		}
	}

	if rc.mode == "oauth" {
		return runOAuthOne(ctx, acc, rc, result, proxyURL)
	}

	reg := register.New(ctx, dockerPool, yamlCfg, register.Options{
		TaskID:      taskID,
		Email:       acc.Email,
		Password:    acc.Password,
		IsLogin:     rc.mode == "login",
		ProxyURL:    proxyURL,
		PreferHosts: rc.preferHosts,
	})
	defer reg.Close()

	reg.SetStatusCallback(func(status, message string, data map[string]interface{}) {
		log.Printf("[%s] %s: %s", taskID, status, message)
	})
	reg.SetScreenshotCallback(func(step string, png []byte) {
		filename := fmt.Sprintf("%s_%s_%d.png", safeName(acc.Email), safeName(step), time.Now().UnixNano())
		path := filepath.Join(rc.screenshotDir, filename)
		if err := os.WriteFile(path, png, 0o644); err != nil {
			log.Printf("[%s] 保存截图失败: %v", taskID, err)
			return
		}
		log.Printf("[%s] 截图: %s", taskID, path)
	})
	reg.SetCriticalSectionCallback(func(inCritical bool) {
		if inCritical {
			log.Printf("[%s] 进入结果保存阶段", taskID)
		}
	})
	reg.SetVerificationCodeCallback(func() (string, error) {
		return getVerificationCode(acc.Email, rc)
	})

	regResult, err := reg.Run()
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	if regResult == nil || !regResult.Success {
		if regResult != nil && regResult.ErrorMessage != "" {
			result.ErrorMessage = regResult.ErrorMessage
		} else {
			result.ErrorMessage = "执行失败"
		}
		return result
	}

	result.Success = true
	result.Workspaces = regResult.Workspaces
	if rc.includeSecrets {
		result.Cookies = regResult.Cookies
		result.AccessToken = regResult.AccessToken
	}
	return result
}

func runProtocolRegisterOne(ctx context.Context, acc accountInput, rc *runnerConfig, result runResult, proxyURL string) runResult {
	name := register.GenerateRandomName()
	birthday := register.GenerateRandomBirthday()
	regResult, err := protocolregister.Run(ctx, protocolregister.Account{
		Email:     acc.Email,
		Password:  acc.Password,
		Name:      name,
		Birthdate: fmt.Sprintf("%s-%s-%s", birthday.Year, birthday.Month, birthday.Day),
	}, protocolregister.Options{
		ProxyURL:    proxyURL,
		CodeTimeout: 2 * time.Minute,
		Progress: func(detail string) {
			log.Printf("[%s] Register: %s", result.TaskID, detail)
		},
		VerificationCode: func(email string) (string, error) {
			return getVerificationCode(email, rc)
		},
	})
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	workspaceName := "Personal account"
	accessToken := regResult.AccessToken
	workspace := register.WorkspaceSession{
		WorkspaceID:        regResult.WorkspaceID,
		Name:               &workspaceName,
		Structure:          "personal",
		IsTeam:             regResult.IsTeam,
		PlanType:           regResult.PlanType,
		SubscriptionStatus: "none",
		WillRenew:          false,
		SeatsUsed:          0,
		SeatsTotal:         0,
		AccessToken:        &accessToken,
	}
	if regResult.IsTeam {
		workspace.Structure = "workspace"
	}
	if strings.TrimSpace(regResult.OrgID) != "" {
		orgID := strings.TrimSpace(regResult.OrgID)
		workspace.OrganizationID = &orgID
	}
	if strings.TrimSpace(regResult.Expires) != "" {
		expires := strings.TrimSpace(regResult.Expires)
		workspace.TokenExpires = &expires
	}
	workspaces, err := register.WorkspacesToJSON([]register.WorkspaceSession{workspace})
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	cookies, err := json.Marshal(regResult.Cookies)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	result.Success = true
	result.Workspaces = workspaces
	if rc.includeSecrets {
		result.Cookies = string(cookies)
		result.AccessToken = regResult.AccessToken
	}
	return result
}

func runOAuthOne(ctx context.Context, acc accountInput, rc *runnerConfig, result runResult, proxyURL string) runResult {
	token, err := oauth.Run(ctx, oauth.Account{
		Email:          acc.Email,
		Password:       acc.Password,
		WorkspaceID:    rc.workspaceID,
		OrganizationID: rc.organizationID,
	}, oauth.Options{
		ProxyURL:    proxyURL,
		CodeTimeout: 2 * time.Minute,
		Progress: func(detail string) {
			log.Printf("[%s] OAuth: %s", result.TaskID, detail)
		},
		VerificationCode: func(email string) (string, error) {
			return getVerificationCode(email, rc)
		},
	})
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	result.Success = true
	if rc.includeSecrets {
		result.OAuthAccessToken = token.AccessToken
		result.OAuthRefreshToken = token.RefreshToken
	}
	result.OAuthExpiresAt = token.ExpiresAt.UTC().Format(time.RFC3339)
	result.OAuthPlanType = token.PlanType
	result.OrganizationID = token.OrganizationID
	return result
}

func getVerificationCode(email string, rc *runnerConfig) (string, error) {
	if rc.codesPath != "" {
		if code := readCodeFromFile(rc.codesPath, email); code != "" {
			return code, nil
		}
	}
	if !rc.interactive {
		return "", fmt.Errorf("暂无验证码")
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()
	fmt.Printf("请输入 %s 的验证码: ", email)
	line, err := rc.stdin.ReadString('\n')
	if err != nil {
		return "", err
	}
	code := strings.TrimSpace(line)
	if code == "" {
		return "", fmt.Errorf("验证码为空")
	}
	return code, nil
}

func readCodeFromFile(path string, email string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), email) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
