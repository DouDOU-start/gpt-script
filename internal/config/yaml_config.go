package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type DockerHost struct {
	Name          string            `yaml:"name" json:"name"`
	URL           string            `yaml:"url" json:"url"`
	MaxContainers int               `yaml:"max_containers" json:"max_containers"`
	TLS           map[string]string `yaml:"tls,omitempty" json:"tls,omitempty"`
	Enabled       *bool             `yaml:"enabled,omitempty" json:"enabled"`
}

func (h DockerHost) IsEnabled() bool {
	return h.Enabled == nil || *h.Enabled
}

type OpenAIConfig struct {
	BaseURL               string `yaml:"base_url" json:"base_url"`
	AuthBaseURL           string `yaml:"auth_base_url" json:"auth_base_url"`
	UserAgent             string `yaml:"user_agent" json:"user_agent"`
	Locale                string `yaml:"locale" json:"locale"`
	RequestTimeoutSeconds int    `yaml:"request_timeout_seconds" json:"request_timeout_seconds"`
}

type ProxyConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	Mode            string   `yaml:"mode" json:"mode"`
	Proxy           string   `yaml:"proxy" json:"proxy"`
	PoolFile        string   `yaml:"pool_file" json:"pool_file"`
	ApplyToRegister bool     `yaml:"apply_to_register" json:"apply_to_register"`
	ApplyToOAuth    bool     `yaml:"apply_to_oauth" json:"apply_to_oauth"`
	ApplyToTeam     bool     `yaml:"apply_to_team" json:"apply_to_team"`
	ApplyToSub2API  bool     `yaml:"apply_to_sub2api" json:"apply_to_sub2api"`
	RotateOnFailure bool     `yaml:"rotate_on_failure" json:"rotate_on_failure"`
	StatePath       string   `yaml:"state_path" json:"state_path"`
	Proxies         []string `yaml:"proxies" json:"proxies"`
}

type BatchPolicyConfig struct {
	BatchSize                  int `yaml:"batch_size" json:"batch_size"`
	Workers                    int `yaml:"workers" json:"workers"`
	DelayBetweenBatchesSeconds int `yaml:"delay_between_batches_seconds" json:"delay_between_batches_seconds"`
	StaggerSeconds             int `yaml:"stagger_seconds" json:"stagger_seconds"`
}

type BatchConfig struct {
	Register BatchPolicyConfig `yaml:"register" json:"register"`
	Team     BatchPolicyConfig `yaml:"team" json:"team"`
	OAuth    BatchPolicyConfig `yaml:"oauth" json:"oauth"`
	Cleanup  BatchPolicyConfig `yaml:"cleanup" json:"cleanup"`
}

type TeamConfig struct {
	CaptainEmail        string `yaml:"captain_email" json:"captain_email"`
	CaptainWorkspaceID  string `yaml:"captain_workspace_id" json:"captain_workspace_id"`
	CaptainAccessToken  string `yaml:"captain_access_token" json:"captain_access_token"`
	CaptainRefreshToken string `yaml:"captain_refresh_token" json:"captain_refresh_token"`
	SeatType            string `yaml:"seat_type" json:"seat_type"`
}

type Sub2APIConfig struct {
	BaseURL     string `yaml:"base_url" json:"base_url"`
	AdminAPIKey string `yaml:"admin_api_key" json:"admin_api_key"`
	Concurrency int    `yaml:"concurrency" json:"concurrency"`
	Priority    int    `yaml:"priority" json:"priority"`
	GroupIDs    []int  `yaml:"group_ids" json:"group_ids"`
}

type YamlConfig struct {
	DockerHosts     []DockerHost  `yaml:"docker_hosts" json:"docker_hosts"`
	BrowserImage    string        `yaml:"browser_image" json:"browser_image"`
	BrowserBackend  string        `yaml:"browser_backend" json:"browser_backend"`
	BrowserHeadless *bool         `yaml:"browser_headless,omitempty" json:"-"`
	OpenAI          OpenAIConfig  `yaml:"openai" json:"openai"`
	Proxy           ProxyConfig   `yaml:"proxy" json:"proxy"`
	Batch           BatchConfig   `yaml:"batch" json:"batch"`
	Team            TeamConfig    `yaml:"team" json:"team"`
	Sub2API         Sub2APIConfig `yaml:"sub2api" json:"sub2api"`
	RunsDir         string        `yaml:"runs_dir" json:"runs_dir"`
}

type YamlStore struct {
	mu   sync.RWMutex
	path string
	cfg  YamlConfig
}

func LoadYamlStore(path string) (*YamlStore, error) {
	store := &YamlStore{path: path}
	if err := store.reload(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *YamlStore) reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cfg = defaultYamlConfig()
			return nil
		}
		return fmt.Errorf("read config.yaml: %w", err)
	}

	var cfg YamlConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("parse config.yaml: %w", err)
	}

	applyYamlDefaults(&cfg)
	s.cfg = cfg
	return nil
}

func (s *YamlStore) Get() YamlConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (c YamlConfig) BrowserHeadlessValue() bool {
	if c.BrowserHeadless == nil {
		return true
	}
	return *c.BrowserHeadless
}

func defaultYamlConfig() YamlConfig {
	cfg := YamlConfig{}
	applyYamlDefaults(&cfg)
	return cfg
}

func applyYamlDefaults(cfg *YamlConfig) {
	if cfg.DockerHosts == nil {
		cfg.DockerHosts = []DockerHost{}
	}
	if len(cfg.DockerHosts) == 0 {
		cfg.DockerHosts = []DockerHost{
			{
				Name:          "本地 Docker",
				URL:           "unix:///var/run/docker.sock",
				MaxContainers: 2,
			},
		}
	}
	if cfg.BrowserImage == "" {
		cfg.BrowserImage = "chromedp/headless-shell:latest"
	}
	if cfg.BrowserBackend == "" {
		cfg.BrowserBackend = "docker"
	}
	if cfg.OpenAI.BaseURL == "" {
		cfg.OpenAI.BaseURL = "https://chatgpt.com"
	}
	if cfg.OpenAI.AuthBaseURL == "" {
		cfg.OpenAI.AuthBaseURL = "https://auth.openai.com"
	}
	if cfg.OpenAI.Locale == "" {
		cfg.OpenAI.Locale = "en-US,en;q=0.9"
	}
	if cfg.OpenAI.RequestTimeoutSeconds <= 0 {
		cfg.OpenAI.RequestTimeoutSeconds = 30
	}
	if cfg.Proxy.Mode == "" {
		cfg.Proxy.Mode = "single"
	}
	if cfg.Proxy.ApplyToRegister == false && cfg.Proxy.ApplyToOAuth == false && cfg.Proxy.ApplyToTeam == false && cfg.Proxy.ApplyToSub2API == false {
		cfg.Proxy.ApplyToRegister = true
		cfg.Proxy.ApplyToOAuth = true
		cfg.Proxy.ApplyToTeam = true
		cfg.Proxy.ApplyToSub2API = true
	}
	if cfg.Batch.Register.Workers <= 0 {
		cfg.Batch.Register.Workers = 1
	}
	if cfg.Batch.OAuth.Workers <= 0 {
		cfg.Batch.OAuth.Workers = 1
	}
	if cfg.Team.SeatType == "" {
		cfg.Team.SeatType = "usage_based"
	}
	if cfg.Sub2API.Concurrency <= 0 {
		cfg.Sub2API.Concurrency = 3
	}
	if cfg.Sub2API.Priority <= 0 {
		cfg.Sub2API.Priority = 1
	}
	if len(cfg.Sub2API.GroupIDs) == 0 {
		cfg.Sub2API.GroupIDs = []int{14}
	}
	if cfg.RunsDir == "" {
		cfg.RunsDir = "runs"
	}
}
