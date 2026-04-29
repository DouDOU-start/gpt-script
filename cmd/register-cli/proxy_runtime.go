package main

import (
	"os"
	"path/filepath"
	"strings"

	"chatgpt-register-script/internal/config"
	migratedproxy "chatgpt-register-script/internal/proxy"
)

func buildProxyPool(cfg config.YamlConfig, configPath string) *migratedproxy.Pool {
	proxies := append([]string(nil), cfg.Proxy.Proxies...)
	if strings.TrimSpace(cfg.Proxy.Proxy) != "" {
		proxies = append(proxies, strings.TrimSpace(cfg.Proxy.Proxy))
	}
	if strings.TrimSpace(cfg.Proxy.PoolFile) != "" {
		poolPath := strings.TrimSpace(cfg.Proxy.PoolFile)
		if !filepath.IsAbs(poolPath) {
			poolPath = filepath.Join(filepath.Dir(configPath), poolPath)
		}
		if payload, err := os.ReadFile(poolPath); err == nil {
			for _, line := range strings.Split(string(payload), "\n") {
				if value := strings.TrimSpace(line); value != "" && !strings.HasPrefix(value, "#") {
					proxies = append(proxies, value)
				}
			}
		}
	}
	return migratedproxy.NewPool(migratedproxy.PoolConfig{
		Enabled: cfg.Proxy.Enabled && len(proxies) > 0,
		Proxies: proxies,
		StageFlags: map[migratedproxy.Stage]bool{
			migratedproxy.StageRegister: cfg.Proxy.ApplyToRegister,
			migratedproxy.StageOAuth:    cfg.Proxy.ApplyToOAuth,
			migratedproxy.StageTeam:     cfg.Proxy.ApplyToTeam,
			migratedproxy.StageSub2API:  cfg.Proxy.ApplyToSub2API,
		},
		RotateOnFailure: cfg.Proxy.RotateOnFailure,
		StatePath:       cfg.Proxy.StatePath,
	})
}
