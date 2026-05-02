// Package register 提供 ChatGPT 账号注册/登录的自动化流程
package register

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"chatgpt-register-script/internal/automation"
	"chatgpt-register-script/internal/config"
	"chatgpt-register-script/internal/dockerpool"
)

// PersonalInfo 个人信息
type PersonalInfo struct {
	FullName string
	Birthday Birthday
}

// Birthday 生日信息
type Birthday struct {
	Month string // 两位数月份，如 "06"
	Day   string // 两位数日期，如 "15"
	Year  string // 四位数年份，如 "1995"
}

// Result 注册/登录结果
type Result struct {
	Success       bool
	AccessToken   string
	Workspaces    string // JSON 格式
	Cookies       string // JSON 格式
	ErrorMessage  string
	AccountBanned bool
}

// StatusCallback 状态回调函数类型
type StatusCallback func(status, message string, data map[string]interface{})

// ScreenshotCallback 截图回调函数类型
type ScreenshotCallback func(step string, screenshot []byte)

// CriticalSectionCallback 关键阶段回调函数类型（进入/退出保存账号的关键阶段）
type CriticalSectionCallback func(inCritical bool)

// VerificationCodeCallback 验证码获取回调函数类型
type VerificationCodeCallback func() (string, error)

// ChatGPTRegister ChatGPT 注册器
type ChatGPTRegister struct {
	taskID       string
	email        string
	password     string
	personalInfo PersonalInfo
	isLogin      bool     // 是否为登录操作（而非注册）
	canceled     bool     // 是否已取消
	stepCount    int      // 步骤计数器，用于截图命名
	proxyURL     string   // 代理 URL
	preferHosts  []string // 优先使用的 Docker 主机名列表

	// 浏览器相关
	browser    *automation.Browser
	docker     *dockerpool.Pool
	yamlConfig config.YamlConfig
	ctx        context.Context

	// 回调函数
	verificationCodeFn      VerificationCodeCallback
	statusCallback          StatusCallback
	screenshotCallback      ScreenshotCallback
	criticalSectionCallback CriticalSectionCallback

	// 工作空间信息缓存（登录后可能需要选择工作空间）
	cachedOrgID       string
	cachedWorkspaceID string

	// 后台监控相关（Cloudflare 检测 + 连接断开检测）
	cfDetected    bool       // 是否检测到 Cloudflare
	disconnected  bool       // 浏览器连接是否断开
	monitorMu     sync.Mutex // 保护监控状态的并发访问
	cfStopCh      chan struct{}
	monitorCtx    context.Context
	monitorCancel context.CancelFunc
}

// Options 创建注册器的选项
type Options struct {
	TaskID       string
	Email        string
	Password     string
	PersonalInfo PersonalInfo
	IsLogin      bool
	ProxyURL     string
	PreferHosts  []string // 优先使用的 Docker 主机名列表
}

// New 创建注册器
func New(ctx context.Context, docker *dockerpool.Pool, yamlCfg config.YamlConfig, opts Options) *ChatGPTRegister {
	info := opts.PersonalInfo
	// 如果未提供个人信息，生成随机信息
	if info.FullName == "" {
		info.FullName = GenerateRandomName()
	}
	if info.Birthday.Year == "" {
		info.Birthday = GenerateRandomBirthday()
	}

	return &ChatGPTRegister{
		taskID:       opts.TaskID,
		email:        opts.Email,
		password:     opts.Password,
		personalInfo: info,
		isLogin:      opts.IsLogin,
		proxyURL:     opts.ProxyURL,
		preferHosts:  opts.PreferHosts,
		docker:       docker,
		yamlConfig:   yamlCfg,
		ctx:          ctx,
	}
}

// SetVerificationCodeCallback 设置验证码回调函数
func (r *ChatGPTRegister) SetVerificationCodeCallback(fn VerificationCodeCallback) {
	r.verificationCodeFn = fn
}

// SetStatusCallback 设置状态回调函数
func (r *ChatGPTRegister) SetStatusCallback(fn StatusCallback) {
	r.statusCallback = fn
}

// SetScreenshotCallback 设置截图回调函数
func (r *ChatGPTRegister) SetScreenshotCallback(fn ScreenshotCallback) {
	r.screenshotCallback = fn
}

// SetCriticalSectionCallback 设置关键阶段回调函数
func (r *ChatGPTRegister) SetCriticalSectionCallback(fn CriticalSectionCallback) {
	r.criticalSectionCallback = fn
}

// SetLoginMode 设置登录模式
func (r *ChatGPTRegister) SetLoginMode(isLogin bool) {
	r.isLogin = isLogin
}

// stepTimeout 返回每个步骤的超时时间
func stepTimeout(name string) time.Duration {
	switch name {
	case "setup":
		return 2 * time.Minute // 包含浏览器启动、Docker 容器分配
	case "navigate":
		return 30 * time.Second
	case "click_signup":
		return 30 * time.Second
	case "input_email":
		return 30 * time.Second
	case "input_password":
		return 30 * time.Second
	case "verify", "verify_if_needed":
		return 2 * time.Minute // 等待验证码
	case "personal_info":
		return 60 * time.Second
	default:
		return 2 * time.Minute
	}
}

// runStepWithTimeout 带超时执行步骤，同时响应 context 取消
func runStepWithTimeout(ctx context.Context, fn func() error, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("步骤发生 panic: %v", r)
			}
		}()
		done <- fn()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("步骤超时（%s）", timeout)
	case <-ctx.Done():
		return fmt.Errorf("任务已取消: %w", ctx.Err())
	}
}

// Run 执行注册/登录流程
func (r *ChatGPTRegister) Run() (*Result, error) {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"setup", r.setupBrowser},
		{"navigate", r.navigateToSignup},
		{"click_signup", r.clickSignupButton},
		{"input_email", r.inputEmail},
		{"input_password", r.inputPassword},
	}

	// 注册操作需要验证码和个人信息
	if !r.isLogin {
		steps = append(steps,
			struct {
				name string
				fn   func() error
			}{"verify", r.handleVerification},
			struct {
				name string
				fn   func() error
			}{"personal_info", r.fillPersonalInfo},
		)
	} else {
		// 登录操作可能需要验证码
		steps = append(steps,
			struct {
				name string
				fn   func() error
			}{"verify_if_needed", r.handleLoginVerification},
		)
	}

	for _, step := range steps {
		if r.canceled {
			return nil, fmt.Errorf("任务已取消")
		}

		// 检查浏览器连接是否断开（由后台监控设置）
		if r.isBrowserDisconnectedFlag() {
			err := fmt.Errorf("浏览器连接已断开")
			r.sendError(step.name, err.Error())
			return &Result{
				Success:      false,
				ErrorMessage: err.Error(),
			}, err
		}

		// 检查是否检测到 Cloudflare（由后台监控设置）
		if r.isCloudflareDetected() {
			err := fmt.Errorf("被 Cloudflare 人机验证拦截")
			r.sendScreenshot("检测到 Cloudflare 人机验证")
			r.sendError(step.name, err.Error())
			return &Result{
				Success:      false,
				ErrorMessage: err.Error(),
			}, err
		}

		log.Printf("[%s] 开始执行步骤: %s", r.taskID, step.name)

		// 映射步骤名称到友好的状态和消息
		stepStatus, stepMessage := r.getStepStatusAndMessage(step.name)
		r.sendStatus(stepStatus, stepMessage, nil)

		// 浏览器初始化后启动后台监控
		timeout := stepTimeout(step.name)

		if step.name == "setup" {
			if err := runStepWithTimeout(r.ctx, step.fn, timeout); err != nil {
				log.Printf("[%s] 步骤 %s 失败: %v", r.taskID, step.name, err)
				r.sendScreenshot(fmt.Sprintf("步骤 %s 失败", step.name))
				r.sendError(step.name, err.Error())
				return &Result{
					Success:      false,
					ErrorMessage: err.Error(),
				}, err
			}
			// 启动后台监控（Cloudflare + 连接断开检测）
			r.startBrowserMonitor()
			log.Printf("[%s] 步骤 %s 完成", r.taskID, step.name)
			continue
		}

		if err := runStepWithTimeout(r.ctx, step.fn, timeout); err != nil {
			// 检查是否为连接断开错误
			if isConnectionError(err) {
				r.monitorMu.Lock()
				r.disconnected = true
				r.monitorMu.Unlock()
				log.Printf("[%s] 步骤 %s 失败: 浏览器连接断开", r.taskID, step.name)
				r.sendError(step.name, "浏览器连接已断开")
				return &Result{
					Success:      false,
					ErrorMessage: "浏览器连接已断开",
				}, fmt.Errorf("浏览器连接已断开")
			}

			log.Printf("[%s] 步骤 %s 失败: %v", r.taskID, step.name, err)
			r.sendScreenshot(fmt.Sprintf("步骤 %s 失败", step.name))
			r.dumpPageHTML(step.name)
			r.sendError(step.name, err.Error())
			return &Result{
				Success:      false,
				ErrorMessage: err.Error(),
			}, err
		}

		log.Printf("[%s] 步骤 %s 完成", r.taskID, step.name)
	}

	// 获取所有工作空间 session 并构建结果（保持后台监控运行）
	result, err := r.buildResultWithTimeout(3 * time.Minute)

	// buildResult 完成后停止后台监控
	r.stopBrowserMonitor()

	if err != nil {
		return nil, err
	}

	if r.isLogin {
		r.sendStatus("completed", "登录完成", nil)
	} else {
		r.sendStatus("completed", "注册完成", nil)
	}

	return result, nil
}

// Cancel 取消任务
func (r *ChatGPTRegister) Cancel() {
	r.canceled = true
	r.Close()
}

// Close 关闭浏览器
func (r *ChatGPTRegister) Close() {
	// 停止后台监控
	r.stopBrowserMonitor()

	if r.browser != nil {
		r.browser.Close(r.ctx)
		r.browser = nil
	}
}

// startBrowserMonitor 启动浏览器后台监控（Cloudflare 检测 + 连接断开检测）
func (r *ChatGPTRegister) startBrowserMonitor() {
	r.monitorCtx, r.monitorCancel = context.WithCancel(r.ctx)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.monitorCtx.Done():
				return
			case <-ticker.C:
				if r.browser == nil {
					return
				}

				// 检测连接是否断开（通过尝试获取页面信息）
				if r.isBrowserDisconnected() {
					r.monitorMu.Lock()
					if !r.disconnected {
						r.disconnected = true
						log.Printf("[%s] 后台监控检测到浏览器连接断开", r.taskID)
					}
					r.monitorMu.Unlock()
					return
				}

				// 检测 Cloudflare 人机验证
				if isCloudflareChallenge(r.browser) {
					r.monitorMu.Lock()
					if !r.cfDetected {
						r.cfDetected = true
						log.Printf("[%s] 后台监控检测到 Cloudflare 人机验证", r.taskID)
					}
					r.monitorMu.Unlock()
					return
				}
			}
		}
	}()
}

// stopBrowserMonitor 停止浏览器后台监控
func (r *ChatGPTRegister) stopBrowserMonitor() {
	if r.monitorCancel != nil {
		r.monitorCancel()
		r.monitorCancel = nil
	}
}

// browserGone 快速检查浏览器是否已不可用（被 Close 置 nil 或标志位已设置）
// 用于轮询循环中的安全退出，避免 nil pointer panic
func (r *ChatGPTRegister) browserGone() bool {
	return r.browser == nil || r.isBrowserDisconnectedFlag()
}

// isBrowserDisconnected 检测浏览器连接是否断开
func (r *ChatGPTRegister) isBrowserDisconnected() bool {
	if r.browser == nil {
		return true
	}
	// 尝试获取页面 URL，如果失败说明连接断开
	_, err := r.browser.GetURL()
	if err != nil && isConnectionError(err) {
		return true
	}
	return false
}

// isConnectionError 判断是否为连接断开错误
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "context canceled")
}

// isCloudflareDetected 检查是否检测到 Cloudflare
func (r *ChatGPTRegister) isCloudflareDetected() bool {
	r.monitorMu.Lock()
	defer r.monitorMu.Unlock()
	return r.cfDetected
}

// isBrowserDisconnectedFlag 检查浏览器连接是否断开（从标志位）
func (r *ChatGPTRegister) isBrowserDisconnectedFlag() bool {
	r.monitorMu.Lock()
	defer r.monitorMu.Unlock()
	return r.disconnected
}

// GetBrowser 获取浏览器（用于复用浏览器会话）
func (r *ChatGPTRegister) GetBrowser() *automation.Browser {
	return r.browser
}

// GetEmail 获取邮箱
func (r *ChatGPTRegister) GetEmail() string {
	return r.email
}

// GetPassword 获取密码
func (r *ChatGPTRegister) GetPassword() string {
	return r.password
}

// IsCanceled 检查是否已取消
func (r *ChatGPTRegister) IsCanceled() bool {
	return r.canceled
}

// buildResultWithTimeout 带超时保护的 buildResult 包装
func (r *ChatGPTRegister) buildResultWithTimeout(timeout time.Duration) (*Result, error) {
	type buildResultOut struct {
		result *Result
		err    error
	}
	done := make(chan buildResultOut, 1)
	go func() {
		defer func() {
			if rv := recover(); rv != nil {
				done <- buildResultOut{nil, fmt.Errorf("buildResult panic: %v", rv)}
			}
		}()
		result, err := r.buildResult()
		done <- buildResultOut{result, err}
	}()
	select {
	case out := <-done:
		return out.result, out.err
	case <-time.After(timeout):
		log.Printf("[%s] buildResult 超时（%s）", r.taskID, timeout)
		return nil, fmt.Errorf("获取工作空间 session 超时（%s）", timeout)
	case <-r.ctx.Done():
		log.Printf("[%s] buildResult 被取消: %v", r.taskID, r.ctx.Err())
		return nil, fmt.Errorf("任务已取消: %w", r.ctx.Err())
	}
}

// buildResult 构建结果：遍历所有工作空间获取 session
func (r *ChatGPTRegister) buildResult() (*Result, error) {
	log.Printf("[%s] buildResult 开始执行", r.taskID)
	if r.browser == nil {
		return nil, fmt.Errorf("浏览器未初始化")
	}

	// 进入关键阶段（遍历工作空间期间不可取消）
	if r.criticalSectionCallback != nil {
		r.criticalSectionCallback(true)
		defer r.criticalSectionCallback(false)
	}

	// 确保已到达 ChatGPT 主页
	currentURL, _ := r.browser.GetURL()
	if !isLikelyOnChatGPTMainPage(currentURL) {
		log.Printf("[%s] 当前未在主页，等待...", r.taskID)
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			if r.browserGone() {
				break
			}
			currentURL, _ = r.browser.GetURL()
			if isLikelyOnChatGPTMainPage(currentURL) {
				break
			}
		}
	}

	log.Printf("[%s] 开始获取所有工作空间 session，当前 URL: %s", r.taskID, currentURL)
	r.sendScreenshot("开始获取所有工作空间 session")

	// 创建 SessionExtractor 和 WorkspaceManager
	se := NewSessionExtractor(r.browser, r.taskID)
	se.OnScreenshot = func(step string) { r.sendScreenshot(step) }

	// 检查页面是否显示封禁信息（在耗时的工作空间遍历之前拦截）
	if err := se.CheckAccountStatus(); err != nil {
		log.Printf("[%s] buildResult 检测到账号被封禁: %v", r.taskID, err)
		r.sendScreenshot("账号已被封禁")
		return nil, &AccountBannedError{Email: r.email}
	}

	wm := NewWorkspaceManager(r.browser, r.taskID)
	wm.OnScreenshot = func(step string) { r.sendScreenshot(step) }

	// 收集所有工作空间的 session
	var allWorkspaces []WorkspaceSession
	var firstAccessToken string

	saveFn := func(name string, session SessionInfo) error {
		log.Printf("[%s] saveFn 收到工作空间: name=%s, workspaceID=%s, planType=%s, isTeam=%v",
			r.taskID, name, session.WorkspaceID, session.PlanType, session.IsTeam)
		if firstAccessToken == "" {
			firstAccessToken = session.AccessToken
		}
		allWorkspaces = append(allWorkspaces, sessionInfoToWorkspaceSession(name, session))
		return nil
	}

	log.Printf("[%s] 调用 SaveAllWorkspaces...", r.taskID)
	se.SaveAllWorkspaces(wm, saveFn)
	log.Printf("[%s] SaveAllWorkspaces 完成，获取到 %d 个工作空间", r.taskID, len(allWorkspaces))

	// 降级：如果未获取到任何工作空间，回退到单次 session 获取
	if len(allWorkspaces) == 0 {
		log.Printf("[%s] SaveAllWorkspaces 未获取到工作空间，降级为单次 session 获取", r.taskID)

		// 降级前再次检查封禁（页面可能在工作空间遍历期间才渲染出封禁文本）
		if err := se.CheckAccountStatus(); err != nil {
			log.Printf("[%s] 降级阶段检测到账号被封禁: %v", r.taskID, err)
			r.sendScreenshot("账号已被封禁")
			return nil, &AccountBannedError{Email: r.email}
		}

		session, err := r.browser.FetchSessionLite()
		if err != nil {
			r.sendScreenshot("获取 session 失败")
			return nil, fmt.Errorf("获取 session 失败: %w", err)
		}
		if session.Error != nil && *session.Error != "" {
			return &Result{
				Success:      false,
				ErrorMessage: *session.Error,
			}, fmt.Errorf("session 错误: %s", *session.Error)
		}
		if session.AccessToken == "" {
			return nil, fmt.Errorf("session 中未找到 access_token")
		}
		workspacesJSON, err := buildWorkspacesFromSession(session)
		if err != nil {
			return nil, fmt.Errorf("解析 session 失败: %w", err)
		}
		cookiesJSON := ""
		if cookies, err := r.browser.GetCookies(); err == nil {
			if raw, err := automation.CookiesToJSON(cookies); err == nil {
				cookiesJSON = raw
			}
		}
		return &Result{
			Success:     true,
			AccessToken: session.AccessToken,
			Workspaces:  workspacesJSON,
			Cookies:     cookiesJSON,
		}, nil
	}

	// 序列化所有工作空间
	workspacesJSON, err := WorkspacesToJSON(allWorkspaces)
	if err != nil {
		return nil, fmt.Errorf("序列化工作空间失败: %w", err)
	}

	// 获取 cookies
	cookiesJSON := ""
	if cookies, err := r.browser.GetCookies(); err == nil {
		if raw, err := automation.CookiesToJSON(cookies); err == nil {
			cookiesJSON = raw
		}
	}

	log.Printf("[%s] 成功获取 %d 个工作空间的 session", r.taskID, len(allWorkspaces))

	return &Result{
		Success:     true,
		AccessToken: firstAccessToken,
		Workspaces:  workspacesJSON,
		Cookies:     cookiesJSON,
	}, nil
}
