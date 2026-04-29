package automation

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"chatgpt-register-script/internal/dockerpool"
)

type Backend interface {
	Create(ctx context.Context, opts BackendOptions) (*Manager, error)
}

type BackendOptions struct {
	TaskID      string
	TaskType    string
	ProxyURL    string
	PreferHosts []string
}

type FactoryBackend struct {
	Factory *ManagerFactory
}

func (b FactoryBackend) Create(ctx context.Context, opts BackendOptions) (*Manager, error) {
	if strings.TrimSpace(opts.TaskType) == "" {
		opts.TaskType = "browser"
	}
	if b.Factory == nil {
		return nil, errNilFactory()
	}
	if b.Factory.docker != nil && strings.TrimSpace(b.Factory.cfg.BrowserBackend) == "docker" {
		return b.Factory.CreateWithDocker(ctx, opts.TaskID, opts.TaskType, dockerStartOptions(opts))
	}
	manager, err := b.Factory.Create(opts.ProxyURL)
	if err != nil {
		return nil, err
	}
	manager.SetContext(ctx)
	return manager, nil
}

func BrowserSandboxEnabled() bool {
	return os.Geteuid() != 0
}

func BrowserHeadlessEnabled(configured bool) bool {
	return configured
}

func CleanupBrowserResidue() map[string]int {
	removedDirs := 0
	for _, pattern := range []string{"/tmp/uc_*", "/tmp/proxy_ext_*"} {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			if os.RemoveAll(path) == nil {
				removedDirs++
			}
		}
	}
	return map[string]int{"temp_dirs": removedDirs}
}

func dockerStartOptions(opts BackendOptions) dockerpool.StartBrowserOptions {
	return dockerpool.StartBrowserOptions{ProxyURL: opts.ProxyURL, PreferHosts: opts.PreferHosts}
}

func errNilFactory() error { return backendError("manager factory is required") }

type backendError string

func (e backendError) Error() string { return string(e) }
