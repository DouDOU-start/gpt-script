package automation

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"

	"chatgpt-register-script/internal/dockerpool"
)

// Manager 浏览器管理器
// 封装了 go-rod 的浏览器操作，提供页面导航、元素操作、截图等功能
type Manager struct {
	browser   *rod.Browser
	page      *rod.Page
	proxyID   string // CDP 代理 ID（如果使用代理模式）
	proxyAuth string // 代理认证头（Base64 编码的 user:pass）

	docker         *dockerpool.Pool // Docker 连接池（用于 browserless 后端）
	dockerSlot     *dockerpool.Slot // Docker slot（用于资源释放）
	browserVersion string           // 浏览器版本（如 "141.0.7390.55"，用于生成匹配的 User-Agent）

	// VNC 信息（仅 VNC 镜像有效）
	vncURL        string // VNC 访问地址
	vncPort       string // VNC 外部端口
	vncHost       string // VNC 主机地址
	containerName string // Docker 容器名称

	onClose   func()             // 关闭时的回调函数
	ctx       context.Context    // 绑定到 page 的 context，取消时自动中断所有 Rod 操作
	ctxCancel context.CancelFunc // 取消函数
}

type Browser = Manager

// SetOnClose 设置关闭时的回调函数
func (m *Manager) SetOnClose(callback func()) {
	m.onClose = callback
}

// GetProxyID 获取 CDP 代理 ID
func (m *Manager) GetProxyID() string {
	return m.proxyID
}

// GetVNCURL 获取 VNC 访问地址（仅 VNC 镜像有效）
func (m *Manager) GetVNCURL() string { return m.vncURL }

// GetVNCPort 获取 VNC 外部端口
func (m *Manager) GetVNCPort() string { return m.vncPort }

// GetVNCHost 获取 VNC 主机地址
func (m *Manager) GetVNCHost() string { return m.vncHost }

// GetContainerName 获取 Docker 容器名称
func (m *Manager) GetContainerName() string { return m.containerName }

// GetPage 获取当前页面对象（用于底层操作）
func (m *Manager) GetPage() *rod.Page { return m.page }

// GetDockerSlot 获取 Docker slot（用于延迟释放场景）
func (m *Manager) GetDockerSlot() *dockerpool.Slot { return m.dockerSlot }

// DetachDockerSlot 分离 Docker slot，调用者接管资源释放职责
// 返回 slot 后，Manager.Close 不再释放 Docker slot
func (m *Manager) DetachDockerSlot() *dockerpool.Slot {
	slot := m.dockerSlot
	m.dockerSlot = nil
	return slot
}

// SetContext 绑定 context 到浏览器管理器
// context 取消时，所有 Rod 操作（Navigate/Click/Eval/Element 等）会自动中断
func (m *Manager) SetContext(ctx context.Context) {
	m.ctx, m.ctxCancel = context.WithCancel(ctx)
}

// CancelContext 主动取消浏览器 context，中断所有正在执行的 Rod 操作
func (m *Manager) CancelContext() {
	if m.ctxCancel != nil {
		m.ctxCancel()
	}
}

// NewPage 创建新页面
func (m *Manager) NewPage() (*rod.Page, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not initialized")
	}
	p, err := stealth.Page(m.browser)
	if err != nil {
		return nil, err
	}

	// 绑定 context：context 取消时自动中断所有 Rod CDP 操作
	if m.ctx != nil {
		p = p.Context(m.ctx)
	}

	// Docker 模式下不覆盖 User-Agent，使用镜像内置的 UA
	// 这样 TLS 指纹与 User-Agent 完全匹配，避免被检测
	// 仅在本地模式（无 browserVersion）时使用随机 UA
	if m.browserVersion == "" {
		ua := dockerpool.RandomUserAgent()
		_ = proto.NetworkSetUserAgentOverride{UserAgent: ua}.Call(p)
	}

	m.page = p
	return p, nil
}

// Close 关闭浏览器并释放资源
// 先取消 context 中断所有正在执行的 Rod 操作，再关闭浏览器连接
func (m *Manager) Close(_ context.Context) {
	const closeTimeout = 5 * time.Second

	// 先取消 context，中断所有正在执行的 Rod 操作（Navigate/Click/Eval 等）
	// 这样泄漏的 goroutine 会立即收到取消信号并返回
	m.CancelContext()

	if m.page != nil {
		done := make(chan struct{}, 1)
		go func() { _ = m.page.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(closeTimeout):
			log.Printf("[Browser] page.Close() 超时（%s），跳过", closeTimeout)
		}
		m.page = nil
	}
	if m.browser != nil {
		done := make(chan struct{}, 1)
		go func() { _ = m.browser.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(closeTimeout):
			log.Printf("[Browser] browser.Close() 超时（%s），跳过", closeTimeout)
		}
		m.browser = nil
	}
	if m.docker != nil && m.dockerSlot != nil {
		m.docker.ReleaseSlot(context.Background(), m.dockerSlot)
		m.dockerSlot = nil
	}
	// 执行关闭回调
	if m.onClose != nil {
		m.onClose()
	}
}

// Navigate 导航到指定 URL
func (m *Manager) Navigate(url string) error {
	if m.page == nil {
		if _, err := m.NewPage(); err != nil {
			return err
		}
	}

	width := 1920
	height := 1080
	_ = m.page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{Width: width, Height: height})

	if err := m.page.Navigate(url); err != nil {
		return err
	}
	// WaitLoad 设置 15 秒超时，避免因页面持续有后台请求而无限等待
	// 超时后仅记录日志，不中断流程（页面主要内容通常已加载完成）
	if err := m.page.Timeout(15 * time.Second).WaitLoad(); err != nil {
		log.Printf("[Browser] WaitLoad 超时（15s），继续执行: %v", err)
	}

	m.acceptCookieConsent()
	randomDelay(800, 1500)
	_, _ = m.page.Eval(`() => { window.scrollBy(0, Math.random() * 100); return true; }`)
	randomDelay(300, 700)
	return nil
}

// acceptCookieConsent 点击 Cookie 同意按钮
func (m *Manager) acceptCookieConsent() {
	if m.page == nil {
		return
	}
	for i := 0; i < 10; i++ {
		result, err := m.page.Eval(`() => {
			const btn = Array.from(document.querySelectorAll('button')).find(
				b => (b.textContent || '').trim() === 'Accept all'
			);
			if (btn) { btn.click(); return true; }
			return false;
		}`)
		if err == nil && result != nil {
			var clicked bool
			if result.Value.Unmarshal(&clicked) == nil && clicked {
				time.Sleep(500 * time.Millisecond)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Click 点击元素
func (m *Manager) Click(selector string) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}
	randomDelay(200, 500)
	el, err := m.page.Element(selector)
	if err != nil {
		return err
	}
	_ = el.ScrollIntoView()
	_ = el.Hover()
	randomDelay(100, 250)
	return el.Click("left", 1)
}

// Type 输入文本（模拟人类输入，适合本地 Docker）
func (m *Manager) Type(selector, text string) error {
	return m.TypeWithSpeed(selector, text, false)
}

// TypeFast 快速输入文本（一次性输入，适合远程 Docker）
func (m *Manager) TypeFast(selector, text string) error {
	return m.TypeWithSpeed(selector, text, true)
}

// TypeWithSpeed 输入文本，可选快速模式
func (m *Manager) TypeWithSpeed(selector, text string, fast bool) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}

	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		el, err := m.page.Element(selector)
		if err != nil {
			return err
		}
		if err := el.Focus(); err != nil {
			return err
		}
		_ = el.SelectAllText()
		_ = el.Input("")
		time.Sleep(300 * time.Millisecond)

		if fast {
			err = el.Input(text)
		} else {
			for _, char := range text {
				if err = el.Input(string(char)); err != nil {
					err = fmt.Errorf("输入字符失败: %w", err)
					break
				}
				time.Sleep(time.Duration(rand.Intn(100)+50) * time.Millisecond)
			}
		}
		if err != nil {
			return err
		}

		// 验证输入值是否完整
		time.Sleep(200 * time.Millisecond)
		val, evalErr := el.Eval(`() => this.value || ''`)
		if evalErr != nil {
			return nil // 无法验证，信任输入结果
		}
		if val.Value.Str() == text {
			return nil
		}

		// 输入不完整，等待后重试
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("输入验证失败：多次重试后输入内容仍不正确")
}

// TypeByIndex 对指定索引的元素输入文本
func (m *Manager) TypeByIndex(selector string, index int, text string) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}
	if index < 0 {
		return fmt.Errorf("invalid index")
	}
	els, err := m.page.Elements(selector)
	if err != nil {
		return err
	}
	if index >= len(els) {
		return fmt.Errorf("element index out of range")
	}
	el := els[index]
	if err := el.Focus(); err != nil {
		return err
	}
	_ = el.SelectAllText()
	_ = el.Input("")
	time.Sleep(300 * time.Millisecond)
	return el.Input(text)
}

// SelectByJS 通过 JS 设置 <select> 元素的值并触发 change 事件
// selector 定位所有 <select> 元素，index 指定第几个，value 是 option 的 value
func (m *Manager) SelectByJS(selector string, index int, value string) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}
	_, err := m.page.Eval(fmt.Sprintf(`() => {
		const els = document.querySelectorAll(%q);
		if (!els || els.length <= %d) throw new Error('select element not found');
		const el = els[%d];
		el.value = %q;
		el.dispatchEvent(new Event('change', {bubbles: true}));
		el.dispatchEvent(new Event('input', {bubbles: true}));
	}`, selector, index, index, value))
	return err
}

// WaitVisible 等待元素可见
func (m *Manager) WaitVisible(selector string, timeout time.Duration) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}
	_, err := m.page.Timeout(timeout).Element(selector)
	return err
}

// IsVisible 检查元素是否可见
func (m *Manager) IsVisible(selector string) bool {
	if m.page == nil {
		return false
	}
	_, err := m.page.Timeout(2 * time.Second).Element(selector)
	return err == nil
}

// Eval 执行 JavaScript
func (m *Manager) Eval(script string, out any) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}
	res, err := m.page.Eval(script)
	if err != nil {
		return err
	}
	if out != nil && res != nil {
		return res.Value.Unmarshal(out)
	}
	return nil
}

// GetURL 获取当前页面 URL
func (m *Manager) GetURL() (string, error) {
	if m.page == nil {
		return "", fmt.Errorf("page not initialized")
	}
	info, err := m.page.Info()
	if err != nil {
		return "", err
	}
	return info.URL, nil
}

// GetTitle 获取当前页面标题
func (m *Manager) GetTitle() (string, error) {
	if m.page == nil {
		return "", fmt.Errorf("page not initialized")
	}
	info, err := m.page.Info()
	if err != nil {
		return "", err
	}
	return info.Title, nil
}

// GetHTML 获取当前页面 HTML
func (m *Manager) GetHTML() (string, error) {
	if m.page == nil {
		return "", fmt.Errorf("page not initialized")
	}
	var html string
	if err := m.Eval(`() => document.documentElement.outerHTML`, &html); err != nil {
		return "", err
	}
	return html, nil
}

// GetText 获取元素文本
func (m *Manager) GetText(selector string) (string, error) {
	if m.page == nil {
		return "", fmt.Errorf("page not initialized")
	}
	elem, err := m.page.Element(selector)
	if err != nil {
		return "", err
	}
	return elem.Text()
}

// GetElements 获取多个元素
func (m *Manager) GetElements(selector string) ([]*rod.Element, error) {
	if m.page == nil {
		return nil, fmt.Errorf("page not initialized")
	}
	return m.page.Elements(selector)
}

// Screenshot 截图（JPEG 格式，质量压缩到 50%）
func (m *Manager) Screenshot() ([]byte, error) {
	if m.page == nil {
		return nil, fmt.Errorf("page not initialized")
	}
	quality := 50
	return m.page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatJpeg,
		Quality: &quality,
	})
}

// ScreenshotPNG 截图（PNG 格式，无压缩）
func (m *Manager) ScreenshotPNG() ([]byte, error) {
	if m.page == nil {
		return nil, fmt.Errorf("page not initialized")
	}
	return m.page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
}

// Reload 刷新当前页面
func (m *Manager) Reload() error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}
	return m.page.Reload()
}

// WaitNavigation 等待页面导航到指定 URL
func (m *Manager) WaitNavigation(urlPattern string, timeout time.Duration) error {
	if m.page == nil {
		return fmt.Errorf("page not initialized")
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := m.page.Info()
		if err != nil {
			return fmt.Errorf("获取页面信息失败: %w", err)
		}
		currentURL := info.URL
		if currentURL == urlPattern || (urlPattern == "" && currentURL != "") {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("等待导航超时")
}

// CheckCloudflare 检查是否遇到 Cloudflare
func (m *Manager) CheckCloudflare() (bool, error) {
	if m.page == nil {
		return false, fmt.Errorf("page not initialized")
	}
	info, err := m.page.Info()
	if err != nil {
		return false, fmt.Errorf("获取页面信息失败: %w", err)
	}
	if info.Title == "Just a moment..." {
		return true, fmt.Errorf("检测到 Cloudflare 验证")
	}
	return false, nil
}

// NewTabNavigate 新开标签页并导航到指定 URL，返回新页面
func (m *Manager) NewTabNavigate(url string) (*rod.Page, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not initialized")
	}

	// 创建新页面（使用 stealth 模式）
	page, err := stealth.Page(m.browser)
	if err != nil {
		return nil, fmt.Errorf("创建新标签页失败: %w", err)
	}

	// 导航到 URL
	if err := page.Navigate(url); err != nil {
		page.Close()
		return nil, fmt.Errorf("导航到 %s 失败: %w", url, err)
	}

	// 等待页面加载
	if err := page.WaitLoad(); err != nil {
		page.Close()
		return nil, fmt.Errorf("等待页面加载失败: %w", err)
	}

	return page, nil
}

// GetCookiesFromPage 从指定页面获取 cookies
func (m *Manager) GetCookiesFromPage(page *rod.Page, urls ...string) ([]Cookie, error) {
	if page == nil {
		return nil, fmt.Errorf("page not initialized")
	}

	cookies, err := proto.NetworkGetCookies{Urls: urls}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("获取 cookies 失败: %w", err)
	}

	var result []Cookie
	for _, c := range cookies.Cookies {
		result = append(result, Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			SameSite: string(c.SameSite),
		})
	}
	return result, nil
}

// randomDelay 随机延迟（内部使用）
func randomDelay(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	d := rand.Intn(maxMs-minMs) + minMs
	time.Sleep(time.Duration(d) * time.Millisecond)
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
