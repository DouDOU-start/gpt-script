package dockerpool

import (
	"math/rand"
	"strings"
)

// UserAgents Chrome User-Agent 列表（2026年2月更新）
// 来源: https://microlink.io/user-agents, https://www.whatismybrowser.com/guides/the-latest-user-agent/chrome
var UserAgents = []string{
	// Chrome 144 - Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	// Chrome 144 - macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	// Chrome 144 - Linux
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	// Chrome 143 - Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36",
	// Chrome 143 - macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36",
	// Chrome 143 - Linux
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36",
}

// RandomUserAgent 随机返回一个 User-Agent
func RandomUserAgent() string {
	return UserAgents[rand.Intn(len(UserAgents))]
}

// AntiDetectFlag 反检测参数
type AntiDetectFlag struct {
	Key   string
	Value string // 空字符串表示布尔标志
}

// AntiDetectFlags 反检测参数列表（导出供其他包使用）
// 注意：移除了 disable-web-security 和 disable-extensions，这些会被检测为自动化痕迹
var AntiDetectFlags = []AntiDetectFlag{
	{"disable-blink-features", "AutomationControlled"}, // 核心：隐藏自动化标识
	{"disable-infobars", ""},                           // 禁用信息栏（Chrome 被自动化控制的提示）
	{"disable-dev-shm-usage", ""},                      // Docker 环境必需
	{"no-sandbox", ""},                                 // Docker 环境必需
	{"disable-setuid-sandbox", ""},                     // Docker 环境必需
	{"disable-gpu", ""},                                // headless 模式稳定性
	{"no-first-run", ""},                               // 跳过首次运行向导
	{"disable-default-apps", ""},                       // 禁用默认应用（减少资源占用）
	{"disable-popup-blocking", ""},                     // 禁用弹窗拦截（正常浏览器行为）
	{"disable-prompt-on-repost", ""},                   // 禁用重新提交提示
	{"disable-hang-monitor", ""},                       // 禁用挂起监控
	{"disable-sync", ""},                               // 禁用同步（无需登录 Google）
	{"disable-translate", ""},                          // 禁用翻译提示
	{"metrics-recording-only", ""},                     // 仅记录指标不上传
	{"password-store", "basic"},                        // 使用基本密码存储
	{"use-mock-keychain", ""},                          // 使用模拟钥匙串
	{"no-default-browser-check", ""},                   // 不检查默认浏览器
	{"disable-background-timer-throttling", ""},        // 禁用后台定时器节流
	{"disable-backgrounding-occluded-windows", ""},     // 禁用遮挡窗口后台化
	{"disable-renderer-backgrounding", ""},             // 禁用渲染器后台化
	{"force-color-profile", "srgb"},                    // 强制 sRGB 色彩配置文件
	{"disable-ipc-flooding-protection", ""},            // 禁用 IPC 洪水保护
}

// GetAntiDetectArgsForHeadlessShell 获取 chromedp/headless-shell 的反检测参数
// 注意：
// - chromedp/headless-shell 入口脚本已处理 CDP 端口和 --no-sandbox、--disable-gpu
// - user-agent 参数包含空格，会被 shell 错误解析，改为通过 rod 在运行时设置
// - proxyServer 可选，格式如 http://proxy:8080（不支持内嵌认证，认证通过 rod 处理）
// - 移除了 disable-web-security 和 disable-extensions，这些会被 hCaptcha 检测
func GetAntiDetectArgsForHeadlessShell(proxyServer string) []string {
	// 反检测参数（与 AntiDetectFlags 保持一致，移除可疑标志）
	args := []string{
		// Docker 环境必需参数（显式传递，不依赖入口脚本）
		"--no-sandbox",             // Docker 环境必需
		"--disable-setuid-sandbox", // Docker 环境必需
		"--disable-dev-shm-usage",  // 使用 /tmp 而非 /dev/shm，避免共享内存不足
		"--disable-gpu",            // headless 模式稳定性
		// 反检测参数
		"--disable-blink-features=AutomationControlled", // 核心：隐藏自动化标识
		"--disable-infobars",                            // 禁用信息栏
		"--disable-default-apps",                        // 禁用默认应用
		"--disable-popup-blocking",                      // 禁用弹窗拦截
		"--disable-prompt-on-repost",                    // 禁用重新提交提示
		"--disable-hang-monitor",                        // 禁用挂起监控
		"--disable-sync",                                // 禁用同步
		"--disable-translate",                           // 禁用翻译
		"--metrics-recording-only",                      // 仅记录指标
		"--password-store=basic",                        // 基本密码存储
		"--use-mock-keychain",                           // 模拟钥匙串
		"--no-default-browser-check",                    // 不检查默认浏览器
		"--no-first-run",                                // 跳过首次运行
		"--disable-background-timer-throttling",         // 禁用后台定时器节流
		"--disable-backgrounding-occluded-windows",      // 禁用遮挡窗口后台化
		"--disable-renderer-backgrounding",              // 禁用渲染器后台化
		"--force-color-profile=srgb",                    // 强制 sRGB
		"--disable-ipc-flooding-protection",             // 禁用 IPC 洪水保护
		"--remote-allow-origins=*",                      // 允许外部 CDP 连接
	}

	// 添加代理配置（提取不含认证的地址）
	if proxyServer != "" {
		proxyAddr, _, _ := ParseProxyURL(proxyServer)
		if proxyAddr != "" {
			args = append(args, "--proxy-server="+proxyAddr)
		}
	} else {
		// 明确禁用代理，避免系统代理干扰
		args = append(args, "--no-proxy-server")
	}

	return args
}

// GetBrowserlessEnvVars 获取 browserless/chrome 的环境变量配置
// 注意：移除了 disable-web-security、disable-extensions、disable-automation 等可疑标志
func GetBrowserlessEnvVars() []string {
	// 构建启动参数 JSON 数组
	launchArgs := []string{
		"--disable-blink-features=AutomationControlled",
		"--disable-infobars",
		"--disable-dev-shm-usage",
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-default-apps",
		"--no-first-run",
		"--disable-gpu",
		"--window-size=1920,1080",
		"--start-maximized",
		"--lang=en-US",
		"--disable-translate",
		"--disable-sync",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-hang-monitor",
		"--disable-ipc-flooding-protection",
		"--password-store=basic",
		"--use-mock-keychain",
		"--force-color-profile=srgb",
		"--metrics-recording-only",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
	}

	// 构建 JSON 数组字符串
	var sb strings.Builder
	sb.WriteString("[")
	for i, arg := range launchArgs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"`)
		sb.WriteString(arg)
		sb.WriteString(`"`)
	}
	sb.WriteString("]")

	return []string{
		"DISPLAY=:99",
		"CONNECTION_TIMEOUT=600000",
		"HEADLESS=false",
		"DEFAULT_LAUNCH_ARGS=" + sb.String(),
		"DEFAULT_USER_AGENT=" + RandomUserAgent(),
		"DEFAULT_STEALTH=true", // 启用 browserless 内置的 stealth 模式
	}
}

// GetChromeVNCEnvVars 获取 chrome-vnc 镜像的环境变量配置
func GetChromeVNCEnvVars(proxyServer string) []string {
	env := []string{
		"SCREEN_WIDTH=1920",
		"SCREEN_HEIGHT=1080",
	}

	if proxyServer != "" {
		proxyAddr, _, _ := ParseProxyURL(proxyServer)
		if proxyAddr != "" {
			env = append(env, "PROXY_SERVER="+proxyAddr)
		}
	}

	return env
}

// GetSeleniumEnvVars 获取 selenium/standalone-chrome 的环境变量配置
func GetSeleniumEnvVars() []string {
	return []string{
		"SE_NODE_SESSION_TIMEOUT=600",
		"SE_VNC_NO_PASSWORD=1",
	}
}

// GetKasmWebEnvVars 获取 kasmweb/chrome 的环境变量配置
func GetKasmWebEnvVars() []string {
	return []string{
		"VNC_PW=chatgpt-register",
		"CHROME_ARGS=--no-sandbox --disable-dev-shm-usage --disable-gpu --remote-debugging-port=9222",
	}
}

// BuildAntiDetectArg 从 AntiDetectFlag 构建命令行参数
func BuildAntiDetectArg(flag AntiDetectFlag) string {
	if flag.Value == "" {
		return "--" + flag.Key
	}
	return "--" + flag.Key + "=" + flag.Value
}

// GetAllAntiDetectArgs 获取所有反检测命令行参数
func GetAllAntiDetectArgs() []string {
	args := make([]string, 0, len(AntiDetectFlags))
	for _, flag := range AntiDetectFlags {
		args = append(args, BuildAntiDetectArg(flag))
	}
	return args
}
