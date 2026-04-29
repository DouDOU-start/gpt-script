package main

import (
	"bufio"
	"sync"

	migratedproxy "chatgpt-register-script/internal/proxy"
	migratedruntime "chatgpt-register-script/internal/runtime"
)

type accountInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Index    int    `json:"index,omitempty"`
}

type runResult struct {
	TaskID            string `json:"task_id"`
	Email             string `json:"email"`
	Password          string `json:"password,omitempty"`
	Mode              string `json:"mode"`
	Success           bool   `json:"success"`
	Workspaces        string `json:"workspaces,omitempty"`
	Cookies           string `json:"cookies,omitempty"`
	AccessToken       string `json:"access_token,omitempty"`
	OAuthAccessToken  string `json:"oauth_access_token,omitempty"`
	OAuthRefreshToken string `json:"oauth_refresh_token,omitempty"`
	OAuthExpiresAt    string `json:"oauth_expires_at,omitempty"`
	OAuthPlanType     string `json:"oauth_plan_type,omitempty"`
	OrganizationID    string `json:"organization_id,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
	StartedAt         string `json:"started_at"`
	CompletedAt       string `json:"completed_at"`
}

type runnerConfig struct {
	mode           string
	proxyURL       string
	screenshotDir  string
	includeSecrets bool
	interactive    bool
	codesPath      string
	preferHosts    []string
	workspaceID    string
	organizationID string
	runsDir        string
	runStore       *migratedruntime.RunStore
	proxyPool      *migratedproxy.Pool
	mu             sync.Mutex
	stdin          *bufio.Reader
}
