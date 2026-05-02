package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ProbeConfig struct {
	BaseURL   string
	UserAgent string
	Locale    string
	Timeout   time.Duration
}

type ProbeResult struct {
	Proxy         string `json:"proxy"`
	OK            bool   `json:"ok"`
	Detail        string `json:"detail"`
	ResolvedProxy string `json:"resolved_proxy,omitempty"`
}

func ProbeRegisterProxy(ctx context.Context, cfg ProbeConfig, proxyValue string) ProbeResult {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://chatgpt.com"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyValue) != "" {
		parsed, err := url.Parse(strings.TrimSpace(proxyValue))
		if err != nil {
			return ProbeResult{Proxy: proxyValue, Detail: err.Error()}
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{Transport: transport, Timeout: cfg.Timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/api/auth/csrf", nil)
	if err != nil {
		return ProbeResult{Proxy: proxyValue, Detail: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", strings.TrimRight(cfg.BaseURL, "/"))
	req.Header.Set("Referer", strings.TrimRight(cfg.BaseURL, "/")+"/")
	if cfg.UserAgent != "" {
		req.Header.Set("User-Agent", cfg.UserAgent)
	}
	if cfg.Locale != "" {
		req.Header.Set("Accept-Language", cfg.Locale)
	}
	res, err := client.Do(req)
	if err != nil {
		return ProbeResult{Proxy: proxyValue, Detail: err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK && strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "json") {
		return ProbeResult{Proxy: proxyValue, OK: true, Detail: "csrf_ok", ResolvedProxy: proxyValue}
	}
	return ProbeResult{Proxy: proxyValue, Detail: fmt.Sprintf("http=%d", res.StatusCode)}
}

func ProbeCandidateProxy(ctx context.Context, cfg ProbeConfig, proxyValue string) ProbeResult {
	candidates := ProxyCandidates(proxyValue)
	failures := []string{}
	for _, candidate := range candidates {
		result := ProbeRegisterProxy(ctx, cfg, candidate)
		if result.OK {
			result.Proxy = proxyValue
			result.ResolvedProxy = candidate
			return result
		}
		failures = append(failures, result.Detail)
	}
	return ProbeResult{Proxy: proxyValue, Detail: strings.Join(failures, " | ")}
}

func ProbeRegisterProxies(ctx context.Context, cfg ProbeConfig, proxies []string, maxWorkers int) []ProbeResult {
	if maxWorkers <= 0 {
		maxWorkers = 6
	}
	if maxWorkers > len(proxies) {
		maxWorkers = len(proxies)
	}
	if maxWorkers == 0 {
		return nil
	}
	results := make([]ProbeResult, len(proxies))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				results[idx] = ProbeCandidateProxy(ctx, cfg, proxies[idx])
			}
		}()
	}
	for idx := range proxies {
		select {
		case <-ctx.Done():
			results[idx] = ProbeResult{Proxy: proxies[idx], Detail: ctx.Err().Error()}
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

func BuildRegisterProxyPool(ctx context.Context, pool *Pool, cfg ProbeConfig, requiredCount, maxWorkers int) (*Pool, []ProbeResult) {
	if pool == nil || pool.ProxyCount() == 0 {
		return pool, nil
	}
	working := []string{}
	results := []ProbeResult{}
	for _, result := range ProbeRegisterProxies(ctx, cfg, pool.AllProxies(), maxWorkers) {
		results = append(results, result)
		if result.OK && result.ResolvedProxy != "" {
			working = append(working, result.ResolvedProxy)
			if requiredCount > 0 && len(working) >= requiredCount {
				break
			}
		}
	}
	return pool.WithProxies(working), results
}

func ResolveRuntimeProxy(ctx context.Context, cfg ProbeConfig, proxyValue string) (string, error) {
	if strings.TrimSpace(proxyValue) == "" || strings.Contains(proxyValue, "://") {
		return proxyValue, nil
	}
	result := ProbeCandidateProxy(ctx, cfg, proxyValue)
	if result.OK && result.ResolvedProxy != "" {
		return result.ResolvedProxy, nil
	}
	return "", fmt.Errorf("proxy scheme detection failed: %s", result.Detail)
}

func ProxyCandidates(proxyValue string) []string {
	stripped := strings.TrimSpace(proxyValue)
	if stripped == "" {
		return nil
	}
	if strings.Contains(stripped, "://") {
		return []string{stripped}
	}
	parts := strings.Split(stripped, ":")
	if len(parts) == 4 && parts[0] != "" && parts[1] != "" && parts[2] != "" && parts[3] != "" {
		userinfo := url.UserPassword(parts[2], parts[3]).String()
		authority := userinfo + "@" + parts[0] + ":" + parts[1]
		return []string{"http://" + authority, "https://" + authority, "socks5h://" + authority}
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		authority := parts[0] + ":" + parts[1]
		return []string{"http://" + authority, "https://" + authority, "socks5h://" + authority}
	}
	return []string{stripped}
}
