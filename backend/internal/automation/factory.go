// Package automation 浏览器自动化管理
package automation

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"

	"chatgpt-register-script/internal/config"
	"chatgpt-register-script/internal/dockerpool"
)

// ManagerFactory 浏览器管理器工厂
// 支持本地浏览器和 Docker 远程浏览器两种脚本运行模式。
type ManagerFactory struct {
	docker   *dockerpool.Pool // Docker 连接池（可选，用于 browserless 后端）
	cfg      config.YamlConfig
	headless bool
}

// NewManagerFactory 创建浏览器管理器工厂
// docker: Docker 连接池（可为 nil，此时仅支持本地模式）
// cfg: YAML 配置
func NewManagerFactory(docker *dockerpool.Pool, cfg config.YamlConfig) *ManagerFactory {
	return &ManagerFactory{
		docker:   docker,
		cfg:      cfg,
		headless: cfg.BrowserHeadlessValue(),
	}
}

// WithHeadless 设置无头模式
func (f *ManagerFactory) WithHeadless(headless bool) *ManagerFactory {
	f.headless = headless
	return f
}

// Create 创建本地浏览器管理器（本地模式）
// 在本地启动 Chrome/Chromium 浏览器
// proxyURL: 可选的代理服务器地址（如 http://user:pass@host:port）
func (f *ManagerFactory) Create(proxyURL string) (*Manager, error) {
	browser, proxyAuth, err := f.launchLocal(proxyURL)
	if err != nil {
		return nil, err
	}
	return &Manager{browser: browser, proxyAuth: proxyAuth}, nil
}

// CreateWithDocker 使用 Docker 容器创建浏览器管理器
// 这是一个便捷方法，自动处理 Docker 容器的启动和管理
// ctx: 上下文
// taskID: 任务 ID（用于追踪 Docker slot）
// taskType: 任务类型（register/refresh/oauth）
// opts: Docker 启动选项
func (f *ManagerFactory) CreateWithDocker(ctx context.Context, taskID, taskType string, opts dockerpool.StartBrowserOptions) (*Manager, error) {
	if f.docker == nil {
		return nil, fmt.Errorf("docker pool is not initialized")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required for docker backend")
	}

	slot := f.docker.AcquireSlot(taskID, taskType, opts.PreferHosts...)
	if slot == nil {
		return nil, fmt.Errorf("no available docker slot")
	}

	// 设置默认镜像
	if strings.TrimSpace(opts.Image) == "" {
		opts.Image = f.cfg.BrowserImage
	}

	container, err := f.docker.StartBrowserContainer(ctx, slot, opts)
	if err != nil {
		f.docker.ReleaseSlot(ctx, slot)
		return nil, err
	}

	controlURL := strings.TrimSpace(container.WSURL)
	if controlURL == "" {
		controlURL = strings.TrimSpace(container.CDPURL)
	}

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		f.docker.ReleaseSlot(ctx, slot)
		return nil, fmt.Errorf("connect cdp: %w", err)
	}

	// 设置代理认证
	proxyAuth := setupProxyAuth(browser, opts.ProxyURL)

	return &Manager{
		browser:        browser,
		proxyAuth:      proxyAuth,
		docker:         f.docker,
		dockerSlot:     slot,
		browserVersion: container.BrowserVersion, // 传递浏览器版本用于生成匹配的 User-Agent
		vncURL:         container.VNCURL,
		vncPort:        container.VNCPort,
		vncHost:        container.VNCHost,
		containerName:  container.ContainerName,
	}, nil
}

// launchLocal 本地启动浏览器
// 返回: browser, proxyAuth (Base64), error
func (f *ManagerFactory) launchLocal(proxyURL string) (*rod.Browser, string, error) {
	// 应用默认值，使用随机 User-Agent 增强反检测能力
	userAgent := dockerpool.RandomUserAgent()
	width := 1920
	height := 1080
	locale := "en-US"

	// 获取浏览器路径
	browserPath, _ := launcher.LookPath()

	var l *launcher.Launcher
	if browserPath != "" {
		// 使用指定路径的 Chrome
		l = launcher.New().Bin(browserPath).Headless(f.headless)
	} else {
		// 回退到默认（会下载 Chromium）
		l = launcher.New().Headless(f.headless)
	}

	l = applyAntiDetectFlags(l)
	l = l.Set("window-size", fmt.Sprintf("%d,%d", width, height)).
		Set("lang", locale).
		Set("user-agent", userAgent)

	// 处理代理配置（分离地址和认证，地址需在启动前设置）
	if strings.TrimSpace(proxyURL) != "" {
		proxyAddr, _, _ := dockerpool.ParseProxyURL(proxyURL)
		if strings.TrimSpace(proxyAddr) != "" {
			l = l.Proxy(proxyAddr)
		}
	}

	url := l.MustLaunch()
	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return nil, "", fmt.Errorf("连接本地浏览器失败: %v", err)
	}

	// 设置代理认证（连接后设置）
	proxyAuth := setupProxyAuth(browser, proxyURL)

	return browser, proxyAuth, nil
}

// setupProxyAuth 设置代理认证并返回 Base64 编码的认证头
// 如果代理 URL 包含认证信息，则通过 Fetch 域拦截首次请求并响应 407 认证
// HandleAuth 会先同步注册到 CDP（启用 Fetch 拦截），再由 goroutine 等待认证事件
func setupProxyAuth(browser *rod.Browser, proxyURL string) string {
	if strings.TrimSpace(proxyURL) == "" {
		return ""
	}
	_, auth, _ := dockerpool.ParseProxyURL(proxyURL)
	if auth == nil || strings.TrimSpace(auth.Username) == "" {
		return ""
	}
	credentials := auth.Username + ":" + auth.Password

	// HandleAuth 内部会同步调用 Fetch.enable 注册拦截，返回的 wait 函数负责等待事件
	// 因此在 go func 启动前，Fetch 拦截已经注册好，不存在竞态
	wait := browser.HandleAuth(auth.Username, auth.Password)
	go func() {
		_ = wait() // 忽略错误，连接断开时会返回错误
	}()

	return base64.StdEncoding.EncodeToString([]byte(credentials))
}

// applyAntiDetectFlags 应用反检测参数到 launcher
func applyAntiDetectFlags(l *launcher.Launcher) *launcher.Launcher {
	antiDetect := []struct {
		Key   string
		Value string // empty means boolean flag
	}{
		{"disable-blink-features", "AutomationControlled"},
		{"disable-infobars", ""},
		{"disable-dev-shm-usage", ""},
		{"no-sandbox", ""},
		{"disable-setuid-sandbox", ""},
		{"disable-gpu", ""},
		{"no-first-run", ""},
		{"disable-default-apps", ""},
		{"disable-popup-blocking", ""},
		{"disable-prompt-on-repost", ""},
		{"disable-hang-monitor", ""},
		{"disable-sync", ""},
		{"disable-translate", ""},
		{"metrics-recording-only", ""},
		{"password-store", "basic"},
		{"use-mock-keychain", ""},
		{"no-default-browser-check", ""},
		{"disable-background-timer-throttling", ""},
		{"disable-backgrounding-occluded-windows", ""},
		{"disable-renderer-backgrounding", ""},
		{"force-color-profile", "srgb"},
		{"disable-ipc-flooding-protection", ""},
		{"remote-allow-origins", "*"},
	}

	for _, f := range antiDetect {
		if f.Value == "" {
			l = l.Set(flags.Flag(f.Key))
			continue
		}
		l = l.Set(flags.Flag(f.Key), f.Value)
	}
	return l
}
