package register

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"chatgpt-register-script/internal/automation"
	"chatgpt-register-script/internal/dockerpool"
)

// setupBrowser 启动浏览器
func (r *ChatGPTRegister) setupBrowser() error {
	taskType := "register"
	if r.isLogin {
		taskType = "refresh"
	}
	factory := automation.NewManagerFactory(r.docker, r.yamlConfig)
	var (
		b   *automation.Manager
		err error
	)
	switch strings.TrimSpace(r.yamlConfig.BrowserBackend) {
	case "docker":
		b, err = factory.CreateWithDocker(r.ctx, r.taskID, taskType, dockerpool.StartBrowserOptions{
			Image:       r.yamlConfig.BrowserImage,
			ProxyURL:    r.proxyURL,
			PreferHosts: r.preferHosts,
		})
	case "local", "":
		b, err = factory.Create(r.proxyURL)
	default:
		err = fmt.Errorf("不支持的浏览器后端: %s", r.yamlConfig.BrowserBackend)
	}
	if err != nil {
		return fmt.Errorf("启动浏览器失败: %w", err)
	}
	// 绑定 context：任务超时/取消时自动中断所有 Rod 浏览器操作
	b.SetContext(r.ctx)
	if err := b.ClearOpenAICookies(); err != nil {
		log.Printf("[%s] 清理 OpenAI/ChatGPT cookies 失败，继续执行: %v", r.taskID, err)
	} else {
		log.Printf("[%s] 已清理 OpenAI/ChatGPT cookies", r.taskID)
	}
	r.browser = b
	return nil
}

// navigateToSignup 导航到 ChatGPT 注册/登录页面
func (r *ChatGPTRegister) navigateToSignup() error {
	if r.browserGone() {
		return fmt.Errorf("浏览器连接已断开")
	}
	if err := r.browser.Navigate("https://chatgpt.com"); err != nil {
		return err
	}
	RandomDelay(1000, 1500)
	r.sendScreenshot("ChatGPT 页面加载完成")
	return nil
}

// clickSignupButton 点击注册/登录按钮，等待邮箱输入框出现
func (r *ChatGPTRegister) clickSignupButton() error {
	if r.browserGone() {
		return fmt.Errorf("浏览器连接已断开")
	}
	RandomDelay(1500, 2500)
	r.sendScreenshot("检查注册入口状态")

	if clicked, err := r.startAuthSessionFromChatGPT(); err != nil {
		log.Printf("[%s] API 引导认证会话失败，回退页面点击: %v", r.taskID, err)
	} else if clicked {
		r.sendScreenshot("已通过 API 引导认证会话")
		RandomDelay(1000, 1800)
	}

	lastScreenshotTime := time.Now()
	for i := 0; i < 60; i++ {
		if r.browserGone() {
			return fmt.Errorf("浏览器连接已断开")
		}
		if isCloudflareChallenge(r.browser) {
			return fmt.Errorf("被 Cloudflare 人机验证拦截")
		}
		if time.Since(lastScreenshotTime) > 3*time.Second {
			r.sendScreenshot("等待注册入口状态...")
			lastScreenshotTime = time.Now()
		}

		state := detectRegisterState(r.browser, r.email)
		log.Printf("[%s] 当前注册入口状态: %s", r.taskID, state)
		switch state {
		case RegisterStateEmail:
			r.sendScreenshot("邮箱输入框已出现")
			return nil
		case RegisterStatePhone:
			clicked, err := switchPhoneToEmail(r.browser)
			if err != nil {
				return fmt.Errorf("切换到邮箱入口失败: %w", err)
			}
			if !clicked {
				return fmt.Errorf("检测到手机号入口，但未找到邮箱入口切换按钮")
			}
			r.sendScreenshot("已切换到邮箱入口")
			RandomDelay(700, 1200)
		case RegisterStatePassword:
			if !pageShowsExpectedEmail(r.browser, r.email) {
				return fmt.Errorf("当前密码页展示邮箱与目标邮箱不一致")
			}
			r.sendScreenshot("已在目标邮箱密码页")
			return nil
		case RegisterStateVerification, RegisterStatePersonalInfo, RegisterStateMain, RegisterStateWorkspace:
			log.Printf("[%s] 已进入后续状态，跳过入口点击: %s", r.taskID, state)
			return nil
		case RegisterStateEmailExists:
			return fmt.Errorf("邮箱已存在或页面提示已有账号")
		case RegisterStateError:
			return fmt.Errorf("认证页面错误: %s", getAuthErrorText(r.browser))
		case RegisterStateLanding, RegisterStateUnknown:
			if clicked, err := clickAuthEntryButton(r.browser, r.isLogin); err != nil {
				return fmt.Errorf("点击注册入口失败: %w", err)
			} else if clicked {
				r.sendScreenshot("已点击注册入口")
				RandomDelay(1000, 1800)
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("等待注册入口超时")
}

func (r *ChatGPTRegister) startAuthSessionFromChatGPT() (bool, error) {
	currentURL, _ := r.browser.GetURL()
	if !isLikelyOnChatGPTMainPage(currentURL) {
		return false, nil
	}
	emailJSON, _ := json.Marshal(r.email)
	script := fmt.Sprintf(`async () => {
		const email = %s;
		const csrf = await fetch('/api/auth/csrf', {
			credentials: 'include',
			headers: { accept: 'application/json' },
		});
		if (!csrf.ok) throw new Error('csrf status ' + csrf.status);
		const csrfPayload = await csrf.json();
		if (!csrfPayload.csrfToken) throw new Error('missing csrf token');
		const params = new URLSearchParams({
			prompt: 'login',
			screen_hint: 'login_or_signup',
			login_hint: email,
			auth_session_logging_id: crypto.randomUUID(),
			'ext-passkey-client-capabilities': '1111',
		});
		const body = new URLSearchParams({
			callbackUrl: 'https://chatgpt.com/',
			csrfToken: csrfPayload.csrfToken,
			json: 'true',
		});
		const signin = await fetch('/api/auth/signin/openai?' + params.toString(), {
			method: 'POST',
			credentials: 'include',
			headers: { accept: 'application/json', 'content-type': 'application/x-www-form-urlencoded' },
			body,
		});
		if (!signin.ok) throw new Error('signin status ' + signin.status);
		const signinPayload = await signin.json();
		if (!signinPayload.url) throw new Error('missing authorize url');
		window.location.href = signinPayload.url;
		return true;
	}`, emailJSON)
	var ok bool
	if err := r.browser.Eval(script, &ok); err != nil {
		return false, err
	}
	return ok, nil
}

// inputEmail 输入邮箱地址
func (r *ChatGPTRegister) inputEmail() error {
	if r.browserGone() {
		return fmt.Errorf("浏览器连接已断开")
	}
	state := detectRegisterState(r.browser, r.email)
	if state == RegisterStatePassword || state == RegisterStateVerification || state == RegisterStatePersonalInfo || state == RegisterStateMain || state == RegisterStateWorkspace {
		log.Printf("[%s] 已进入后续状态，跳过邮箱输入: %s", r.taskID, state)
		return nil
	}
	RandomDelay(500, 1000)
	r.sendScreenshot("准备输入邮箱")

	log.Printf("[%s] 输入邮箱: %s", r.taskID, r.email)
	if err := r.browser.TypeFast(`input[name="email"]`, r.email); err != nil {
		return fmt.Errorf("输入邮箱失败: %w", err)
	}

	RandomDelay(500, 1000)
	r.sendScreenshot("已输入邮箱: " + r.email)

	log.Printf("[%s] 点击 Continue 按钮", r.taskID)
	if err := clickContinueButton(r.browser); err != nil {
		return err
	}

	RandomDelay(500, 1000)
	r.sendScreenshot("已点击继续按钮")

	log.Printf("[%s] 等待邮箱提交后的页面状态...", r.taskID)
	lastScreenshotTime := time.Now()
	for i := 0; i < 40; i++ {
		if r.browserGone() {
			return fmt.Errorf("浏览器连接已断开")
		}
		if isCloudflareChallenge(r.browser) {
			return fmt.Errorf("被 Cloudflare 人机验证拦截")
		}
		if time.Since(lastScreenshotTime) > 3*time.Second {
			r.sendScreenshot("等待邮箱提交结果...")
			lastScreenshotTime = time.Now()
		}

		state := detectRegisterState(r.browser, r.email)
		switch state {
		case RegisterStatePassword:
			r.sendScreenshot("密码输入框已出现")
			return nil
		case RegisterStateVerification:
			r.sendScreenshot("检测到验证码页面")
			return nil
		case RegisterStatePersonalInfo:
			r.sendScreenshot("个人信息表单已出现")
			return nil
		case RegisterStateMain, RegisterStateWorkspace:
			return nil
		case RegisterStatePhone:
			clicked, err := switchPhoneToEmail(r.browser)
			if err != nil {
				return fmt.Errorf("切换到邮箱入口失败: %w", err)
			}
			if !clicked {
				return fmt.Errorf("提交邮箱后进入手机号入口，未找到邮箱切换按钮")
			}
		case RegisterStateEmailExists:
			return fmt.Errorf("邮箱已存在或页面提示已有账号")
		case RegisterStateError:
			return fmt.Errorf("邮箱提交失败: %s", getAuthErrorText(r.browser))
		}

		time.Sleep(500 * time.Millisecond)
	}

	currentURL, _ := r.browser.GetURL()
	log.Printf("[%s] 等待邮箱提交结果超时，当前 URL: %s", r.taskID, currentURL)
	return fmt.Errorf("等待邮箱提交结果超时")
}

// inputPassword 输入密码（智能跳过逻辑）
func (r *ChatGPTRegister) inputPassword() error {
	if r.browserGone() {
		return fmt.Errorf("浏览器连接已断开")
	}

	for attempt := 1; attempt <= 3; attempt++ {
		state := detectRegisterState(r.browser, r.email)
		switch state {
		case RegisterStateVerification, RegisterStatePersonalInfo, RegisterStateMain, RegisterStateWorkspace:
			log.Printf("[%s] 已进入后续状态，跳过密码输入: %s", r.taskID, state)
			return nil
		case RegisterStateError:
			return fmt.Errorf("密码页前置状态错误: %s", getAuthErrorText(r.browser))
		case RegisterStateEmailExists:
			return fmt.Errorf("邮箱已存在或页面提示已有账号")
		}

		if !r.browser.IsVisible(`input[type="password"]`) {
			log.Printf("[%s] 密码输入框不存在，等待加载...", r.taskID)
			if err := r.browser.WaitVisible(`input[type="password"]`, 5*time.Second); err != nil {
				state = detectRegisterState(r.browser, r.email)
				if state == RegisterStateVerification || state == RegisterStatePersonalInfo || state == RegisterStateMain || state == RegisterStateWorkspace {
					return nil
				}
				if isCloudflareChallenge(r.browser) {
					return fmt.Errorf("被 Cloudflare 人机验证拦截")
				}
				return fmt.Errorf("密码输入框未出现")
			}
		}

		r.sendScreenshot(fmt.Sprintf("准备输入密码（第 %d 次）", attempt))
		log.Printf("[%s] 输入密码 (长度: %d, 第 %d 次)", r.taskID, len(r.password), attempt)
		if err := r.browser.TypeFast(`input[type="password"]`, r.password); err != nil {
			return fmt.Errorf("输入密码失败: %w", err)
		}

		RandomDelay(500, 1000)
		r.sendScreenshot("已输入密码")

		if err := clickContinueButton(r.browser); err != nil {
			return err
		}

		RandomDelay(500, 1000)
		r.sendScreenshot("已点击继续按钮")

		outcome, err := r.waitForPasswordSubmitResult(12 * time.Second)
		if err != nil {
			return err
		}
		if outcome != RegisterStatePassword {
			return nil
		}
		if attempt < 3 {
			log.Printf("[%s] 密码提交后仍停留密码页，准备重试", r.taskID)
			RandomDelay(700, 1200)
		}
	}

	return fmt.Errorf("密码提交后仍停留密码页")
}

func (r *ChatGPTRegister) waitForPasswordSubmitResult(timeout time.Duration) (RegisterPageState, error) {
	deadline := time.Now().Add(timeout)
	lastScreenshotTime := time.Now()
	for time.Now().Before(deadline) {
		if r.browserGone() {
			return RegisterStateUnknown, fmt.Errorf("浏览器连接已断开")
		}
		if isCloudflareChallenge(r.browser) {
			return RegisterStateUnknown, fmt.Errorf("被 Cloudflare 人机验证拦截")
		}
		if time.Since(lastScreenshotTime) > 3*time.Second {
			r.sendScreenshot("等待密码提交结果...")
			lastScreenshotTime = time.Now()
		}

		state := detectRegisterState(r.browser, r.email)
		switch state {
		case RegisterStateVerification:
			r.sendScreenshot("已跳转到验证码页面")
			return state, nil
		case RegisterStatePersonalInfo:
			r.sendScreenshot("检测到个人信息表单")
			return state, nil
		case RegisterStateMain:
			r.sendScreenshot("已进入 ChatGPT 主页")
			r.isLogin = true
			return state, nil
		case RegisterStateWorkspace:
			r.sendScreenshot("检测到工作空间选择页面")
			return state, nil
		case RegisterStatePassword:
			if strings.TrimSpace(getAuthErrorText(r.browser)) != "" {
				return state, nil
			}
		case RegisterStateEmailExists:
			return state, fmt.Errorf("邮箱已存在或页面提示已有账号")
		case RegisterStateError:
			return state, fmt.Errorf("密码提交失败: %s", getAuthErrorText(r.browser))
		}

		time.Sleep(500 * time.Millisecond)
	}

	currentURL, _ := r.browser.GetURL()
	log.Printf("[%s] 等待密码提交结果超时，当前 URL: %s", r.taskID, currentURL)
	r.sendScreenshot("等待密码提交结果超时")
	return RegisterStateUnknown, fmt.Errorf("等待密码提交结果超时")
}

// handleLoginVerification 处理登录时的验证码（如果需要）
func (r *ChatGPTRegister) handleLoginVerification() error {
	// 先检查 URL 是否在验证码页面
	currentURL, _ := r.browser.GetURL()
	if strings.Contains(currentURL, "email-verification") {
		log.Printf("[%s] 检测到验证码页面 URL，需要验证码", r.taskID)
		return r.handleVerification()
	}

	// 检查是否需要验证码
	if !IsVerificationCodeVisible(r.browser) {
		log.Printf("[%s] 登录无需验证码", r.taskID)
		return nil
	}

	log.Printf("[%s] 登录需要验证码", r.taskID)
	return r.handleVerification()
}

// handleVerification 处理邮箱验证码
func (r *ChatGPTRegister) handleVerification() error {
	// 等待验证码输入框出现（尝试多种选择器）
	log.Printf("[%s] 等待验证码输入框...", r.taskID)
	codeInputFound := false
	for _, sel := range verificationCodeSelectors {
		if err := r.browser.WaitVisible(sel, 5*time.Second); err == nil {
			log.Printf("[%s] 验证码输入框已出现，选择器: %s", r.taskID, sel)
			codeInputFound = true
			break
		}
	}
	if !codeInputFound && visibleVerificationDigitCount(r.browser) >= 6 {
		codeInputFound = true
		log.Printf("[%s] 验证码分格输入框已出现", r.taskID)
	}
	if !codeInputFound {
		// 检查是否已在主页或个人信息页面（不需要验证码）
		currentURL, _ := r.browser.GetURL()
		if isLikelyOnChatGPTMainPage(currentURL) && hasChatGPTMainFeature(r.browser) {
			log.Printf("[%s] 已在主页，跳过验证步骤", r.taskID)
			return nil
		}
		if r.browser.IsVisible(`input[name="name"]`) || r.browser.IsVisible(`[role="spinbutton"]`) {
			log.Printf("[%s] 已在个人信息页面，跳过验证步骤", r.taskID)
			return nil
		}
		log.Printf("[%s] 未检测到验证码输入框，跳过验证步骤", r.taskID)
		return nil
	}

	r.sendScreenshot("验证码输入框已出现")
	r.sendStatus("waiting_verification", "等待验证码", map[string]interface{}{
		"requires_input": true,
		"input_type":     "verification_code",
	})

	// 使用回调获取验证码
	if r.verificationCodeFn == nil {
		return fmt.Errorf("未设置验证码获取回调")
	}

	const maxWait = 2 * time.Minute
	const pollInterval = 3 * time.Second
	const screenshotInterval = 10 * time.Second
	deadline := time.Now().Add(maxWait)

	log.Printf("[%s] 开始轮询验证码，最长等待 %v", r.taskID, maxWait)

	pollCount := 0
	lastScreenshotTime := time.Now()
	for time.Now().Before(deadline) {
		if r.canceled {
			log.Printf("[%s] 任务已取消，停止等待验证码", r.taskID)
			return fmt.Errorf("任务已取消")
		}

		pollCount++

		// 定期截图
		if time.Since(lastScreenshotTime) > screenshotInterval {
			elapsed := time.Since(deadline.Add(-maxWait))
			r.sendScreenshot(fmt.Sprintf("等待验证码中... (已等待 %d 秒)", int(elapsed.Seconds())))
			lastScreenshotTime = time.Now()
		}

		code, err := r.verificationCodeFn()
		if err != nil {
			// 验证码尚未到达，继续轮询
			if pollCount%10 == 1 {
				log.Printf("[%s] 轮询 #%d: 暂无验证码", r.taskID, pollCount)
			}
			time.Sleep(pollInterval)
			continue
		}

		code = strings.TrimSpace(code)
		if code == "" {
			time.Sleep(pollInterval)
			continue
		}

		log.Printf("[%s] 收到验证码，长度: %d", r.taskID, len(code))
		r.sendScreenshot("收到验证码，准备输入")

		if err := r.inputVerificationCode(code); err != nil {
			return fmt.Errorf("输入验证码失败: %w", err)
		}

		RandomDelay(500, 1000)
		r.sendScreenshot("已输入验证码")

		if err := clickContinueButton(r.browser); err != nil {
			return fmt.Errorf("提交验证码失败: %w", err)
		}

		RandomDelay(500, 1000)
		r.sendScreenshot("已提交验证码")

		return r.waitForVerificationResult()
	}

	return fmt.Errorf("等待验证码超时")
}

func (r *ChatGPTRegister) inputVerificationCode(code string) error {
	selector := getVerificationCodeSelector(r.browser)
	if selector != verificationDigitSelector {
		return r.browser.TypeFast(selector, code)
	}

	var filled bool
	script := fmt.Sprintf(`() => {
		const code = %q;
		const visible = el => {
			const r = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return r.width > 0 && r.height > 0 && style.visibility !== 'hidden' && style.display !== 'none' && !el.disabled;
		};
		const inputs = Array.from(document.querySelectorAll('input[inputmode="numeric"], input[autocomplete="one-time-code"], input[aria-label*="code" i], input[maxlength="1"]')).filter(visible).slice(0, code.length);
		if (inputs.length < code.length) return false;
		for (let i = 0; i < code.length; i++) {
			const input = inputs[i];
			input.focus();
			input.value = code[i];
			input.dispatchEvent(new Event('input', { bubbles: true }));
			input.dispatchEvent(new Event('change', { bubbles: true }));
			input.dispatchEvent(new KeyboardEvent('keyup', { bubbles: true, key: code[i] }));
		}
		return true;
	}`, code)
	if err := r.browser.Eval(script, &filled); err != nil {
		return err
	}
	if !filled {
		return fmt.Errorf("分格验证码输入框数量不足")
	}
	return nil
}

// waitForVerificationResult 等待验证码验证结果
func (r *ChatGPTRegister) waitForVerificationResult() error {
	maxWait := 15 * time.Second
	checkInterval := 500 * time.Millisecond
	startTime := time.Now()

	for time.Since(startTime) < maxWait {
		if r.browserGone() {
			return fmt.Errorf("浏览器连接已断开")
		}
		currentURL, _ := r.browser.GetURL()
		lowerURL := strings.ToLower(currentURL)
		state := detectRegisterState(r.browser, r.email)
		switch state {
		case RegisterStateMain:
			log.Printf("[%s] 验证成功，已进入主页: %s", r.taskID, currentURL)
			return nil
		case RegisterStateWorkspace:
			log.Printf("[%s] 验证成功，到达工作空间选择页面", r.taskID)
			r.sendScreenshot("工作空间选择页面")
			return r.handleWorkspaceSelection()
		case RegisterStatePersonalInfo:
			log.Printf("[%s] 验证成功，到达个人信息页面", r.taskID)
			return nil
		case RegisterStateError:
			return fmt.Errorf("验证码提交后页面错误: %s", getAuthErrorText(r.browser))
		}

		// 如果还在验证页面，检查错误信息
		if strings.Contains(lowerURL, "/email-verification") || state == RegisterStateVerification {
			var pageText string
			_ = r.browser.Eval(`() => document.body.innerText`, &pageText)

			// 检查账号被禁用
			if strings.Contains(pageText, "account_deactivated") || strings.Contains(pageText, "deleted or deactivated") {
				log.Printf("[%s] 账号 %s 已被 OpenAI 停用", r.taskID, r.email)
				r.sendScreenshot("账号已被 OpenAI 停用")
				return &AccountBannedError{Email: r.email}
			}

			// 检查验证码错误
			if strings.Contains(pageText, "incorrect") || strings.Contains(pageText, "invalid code") || strings.Contains(pageText, "wrong code") {
				log.Printf("[%s] 验证码错误", r.taskID)
				r.sendScreenshot("验证码错误")
				return fmt.Errorf("验证码错误")
			}

			// 检查输入框附近的错误样式
			hasError := false
			_ = r.browser.Eval(`() => {
				const selectors = ['input[name="code"]', 'input[type="text"][inputmode="numeric"]', 'input[autocomplete="one-time-code"]'];
				let codeInput = null;
				for (const sel of selectors) {
					codeInput = document.querySelector(sel);
					if (codeInput) break;
				}
				if (!codeInput) return false;
				const form = codeInput.closest('form');
				if (form) {
					const errorInForm = form.querySelector('[class*="error" i], [class*="invalid" i]');
					if (errorInForm) return true;
				}
				return false;
			}`, &hasError)
			if hasError {
				log.Printf("[%s] 验证码输入框显示错误", r.taskID)
				r.sendScreenshot("验证失败，页面显示错误")
				return fmt.Errorf("验证失败，页面显示错误")
			}
		}

		time.Sleep(checkInterval)
	}

	// 超时后再检查一次
	currentURL, _ := r.browser.GetURL()
	log.Printf("[%s] 验证结果检测超时，当前 URL: %s", r.taskID, currentURL)

	if strings.Contains(strings.ToLower(currentURL), "auth.openai.com/workspace") {
		log.Printf("[%s] 验证成功，到达工作空间选择页面", r.taskID)
		return r.handleWorkspaceSelection()
	}

	// 如果 URL 已不在验证页面，认为成功
	if !strings.Contains(strings.ToLower(currentURL), "email-verification") {
		log.Printf("[%s] 验证可能成功，URL 已离开验证页面", r.taskID)
		return nil
	}

	r.sendScreenshot("验证超时")
	return fmt.Errorf("验证超时，请检查截图")
}

// handleWorkspaceSelection 处理 auth.openai.com/workspace 页面的工作空间选择
func (r *ChatGPTRegister) handleWorkspaceSelection() error {
	log.Printf("[%s] 处理工作空间选择页面...", r.taskID)
	RandomDelay(1000, 2000)

	var clicked string
	_ = r.browser.Eval(`() => {
		const buttons = document.querySelectorAll('button');
		for (const btn of buttons) {
			const text = btn.textContent || '';
			if (text.includes('Personal account') || text.includes('Personal workspace')) {
				btn.click();
				return 'personal_button';
			}
		}
		for (const btn of buttons) {
			const text = btn.textContent || '';
			if (text && !text.includes('Terms') && !text.includes('Privacy') &&
				!text.includes('Log out') && !text.includes('Choose') &&
				!text.includes('Workspace') && text.length < 100) {
				btn.click();
				return 'first_button';
			}
		}
		return 'none';
	}`, &clicked)
	log.Printf("[%s] 工作空间选择结果: %s", r.taskID, clicked)
	r.sendScreenshot("已选择工作空间")

	// 等待跳转到 chatgpt.com
	startTime := time.Now()
	for time.Since(startTime) < 15*time.Second {
		if r.browserGone() {
			return fmt.Errorf("浏览器连接已断开")
		}
		currentURL, _ := r.browser.GetURL()
		if isLikelyOnChatGPTMainPage(currentURL) && hasChatGPTMainFeature(r.browser) {
			log.Printf("[%s] 已跳转到 ChatGPT: %s", r.taskID, currentURL)
			RandomDelay(1000, 2000)
			return r.handleChatGPTWorkspaceDialog()
		}
		time.Sleep(500 * time.Millisecond)
	}

	currentURL, _ := r.browser.GetURL()
	log.Printf("[%s] 等待跳转超时，当前 URL: %s", r.taskID, currentURL)
	r.sendScreenshot("等待跳转超时")
	return fmt.Errorf("选择工作空间后跳转超时")
}

// handleChatGPTWorkspaceDialog 处理 chatgpt.com 上的工作空间选择弹窗
func (r *ChatGPTRegister) handleChatGPTWorkspaceDialog() error {
	hasDialog := false
	_ = r.browser.Eval(`() => {
		const dialog = document.querySelector('dialog, [role="dialog"]');
		if (!dialog) return false;
		const text = dialog.textContent || '';
		return text.includes('workspace') || text.includes('Workspace');
	}`, &hasDialog)

	if !hasDialog {
		log.Printf("[%s] 未检测到工作空间选择弹窗，登录完成", r.taskID)
		r.sendScreenshot("已进入 ChatGPT")
		return nil
	}

	log.Printf("[%s] 检测到 chatgpt.com 工作空间选择弹窗", r.taskID)
	r.sendScreenshot("工作空间选择弹窗")

	var result string
	_ = r.browser.Eval(`() => {
		const dialog = document.querySelector('dialog, [role="dialog"]');
		if (!dialog) return 'no_dialog';
		const items = dialog.querySelectorAll('li, [role="listitem"]');
		for (const item of items) {
			const text = item.textContent || '';
			if (text.includes('Personal workspace') || text.includes('Personal account')) {
				const openBtn = item.querySelector('button');
				if (openBtn && openBtn.textContent.includes('Open')) {
					openBtn.click();
					return 'personal_open';
				}
			}
		}
		for (const item of items) {
			const openBtn = item.querySelector('button');
			if (openBtn && openBtn.textContent.includes('Open')) {
				openBtn.click();
				return 'first_open';
			}
		}
		return 'no_open_button';
	}`, &result)

	log.Printf("[%s] 工作空间弹窗点击结果: %s", r.taskID, result)

	if result == "no_dialog" || result == "no_open_button" {
		r.sendScreenshot("弹窗处理异常: " + result)
		return nil
	}

	RandomDelay(1000, 2000)
	r.sendScreenshot("已选择工作空间，登录完成")
	return nil
}

func (r *ChatGPTRegister) acceptPersonalInfoCheckboxes() error {
	var clicked int
	if err := r.browser.Eval(`() => {
		const visible = el => {
			const r = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return r.width > 0 && r.height > 0 && style.visibility !== 'hidden' && style.display !== 'none' && !el.disabled;
		};
		let count = 0;
		for (const input of Array.from(document.querySelectorAll('input[type="checkbox"]')).filter(visible)) {
			if (input.checked) continue;
			const label = input.closest('label') || document.querySelector('label[for="' + input.id + '"]');
			const text = ((label && label.textContent) || input.getAttribute('aria-label') || '').toLowerCase();
			if (!text || /agree|terms|privacy|consent|同意|条款|隐私/.test(text)) {
				input.click();
				count++;
			}
		}
		return count;
	}`, &clicked); err != nil {
		return fmt.Errorf("勾选同意项失败: %w", err)
	}
	if clicked > 0 {
		log.Printf("[%s] 已勾选 %d 个同意项", r.taskID, clicked)
		RandomDelay(300, 500)
	}
	return nil
}

// fillPersonalInfo 填写个人信息（随机姓名+生日）
func (r *ChatGPTRegister) fillPersonalInfo() error {
	RandomDelay(1000, 2000)

	// 检查是否需要填写个人信息（兼容旧版 spinbutton 和新版 select 下拉框）
	if !isPersonalInfoVisible(r.browser) {
		// 可能已经跳过了个人信息步骤
		currentURL, _ := r.browser.GetURL()
		if isLikelyOnChatGPTMainPage(currentURL) && hasChatGPTMainFeature(r.browser) {
			log.Printf("[%s] 已在主页，跳过个人信息填写", r.taskID)
			return nil
		}
		log.Printf("[%s] 未检测到个人信息表单，跳过", r.taskID)
		return nil
	}

	r.sendScreenshot("准备填写个人信息")

	// 填写全名
	if r.browser.IsVisible(`input[name="name"]`) {
		log.Printf("[%s] 填写姓名: %s", r.taskID, r.personalInfo.FullName)
		if err := r.browser.TypeFast(`input[name="name"]`, r.personalInfo.FullName); err != nil {
			return fmt.Errorf("填写姓名失败: %w", err)
		}
		RandomDelay(300, 500)
		r.sendScreenshot("已填写姓名: " + r.personalInfo.FullName)
	}

	if r.browser.IsVisible(`input[name="age"]`) {
		log.Printf("[%s] 填写年龄", r.taskID)
		if err := r.browser.TypeFast(`input[name="age"]`, "19"); err != nil {
			return fmt.Errorf("填写年龄失败: %w", err)
		}
		RandomDelay(300, 500)
	}

	// 填写生日（顺序：月/日/年）
	// 兼容旧版 spinbutton 和新版 select 下拉框
	if r.browser.IsVisible(`[role="spinbutton"]`) {
		// 旧版：spinbutton 输入框
		log.Printf("[%s] 填写生日(spinbutton): %s/%s/%s", r.taskID,
			r.personalInfo.Birthday.Month, r.personalInfo.Birthday.Day, r.personalInfo.Birthday.Year)
		if err := r.browser.TypeByIndex(`[role="spinbutton"]`, 0, r.personalInfo.Birthday.Month); err != nil {
			return fmt.Errorf("填写月份失败: %w", err)
		}
		if err := r.browser.TypeByIndex(`[role="spinbutton"]`, 1, r.personalInfo.Birthday.Day); err != nil {
			return fmt.Errorf("填写日期失败: %w", err)
		}
		if err := r.browser.TypeByIndex(`[role="spinbutton"]`, 2, r.personalInfo.Birthday.Year); err != nil {
			return fmt.Errorf("填写年份失败: %w", err)
		}

		RandomDelay(500, 1000)
		birthdayStr := fmt.Sprintf("%s/%s/%s",
			r.personalInfo.Birthday.Month, r.personalInfo.Birthday.Day, r.personalInfo.Birthday.Year)
		r.sendScreenshot("已填写生日: " + birthdayStr)
	} else if r.browser.IsVisible(`input[name="birthday"]`) || r.browser.IsVisible(`[data-testid="hidden-select-container"] select`) {
		// 新版：select 下拉框（隐藏在 hidden-select-container 内）
		// select option value 不带前导零，需要转换
		monthNum := strings.TrimLeft(r.personalInfo.Birthday.Month, "0")
		dayNum := strings.TrimLeft(r.personalInfo.Birthday.Day, "0")
		yearNum := r.personalInfo.Birthday.Year

		log.Printf("[%s] 填写生日(select): %s/%s/%s", r.taskID, monthNum, dayNum, yearNum)
		selectSelector := `[data-testid="hidden-select-container"] select`
		if err := r.browser.SelectByJS(selectSelector, 0, monthNum); err != nil {
			return fmt.Errorf("选择月份失败: %w", err)
		}
		RandomDelay(200, 400)
		if err := r.browser.SelectByJS(selectSelector, 1, dayNum); err != nil {
			return fmt.Errorf("选择日期失败: %w", err)
		}
		RandomDelay(200, 400)
		if err := r.browser.SelectByJS(selectSelector, 2, yearNum); err != nil {
			return fmt.Errorf("选择年份失败: %w", err)
		}

		RandomDelay(500, 1000)
		birthdayStr := fmt.Sprintf("%s/%s/%s", monthNum, dayNum, yearNum)
		r.sendScreenshot("已填写生日: " + birthdayStr)
	}

	if err := r.acceptPersonalInfoCheckboxes(); err != nil {
		return err
	}

	log.Printf("[%s] 点击继续按钮", r.taskID)
	if err := r.browser.Click(`button[type="submit"]`); err != nil {
		// 回退到 clickContinueButton
		if err2 := clickContinueButton(r.browser); err2 != nil {
			return fmt.Errorf("提交个人信息失败: %w", err)
		}
	}

	RandomDelay(500, 1000)
	r.sendScreenshot("已提交个人信息")

	// 等待页面跳转到主页
	log.Printf("[%s] 等待页面跳转", r.taskID)
	startTime := time.Now()
	lastScreenshotTime := time.Now()
	for time.Since(startTime) < 30*time.Second {
		if r.browserGone() {
			return fmt.Errorf("浏览器连接已断开")
		}
		if time.Since(lastScreenshotTime) > 5*time.Second {
			r.sendScreenshot("等待跳转到主页...")
			lastScreenshotTime = time.Now()
		}

		currentURL, _ := r.browser.GetURL()
		if isLikelyOnChatGPTMainPage(currentURL) && hasChatGPTMainFeature(r.browser) {
			r.sendScreenshot("已跳转到主页")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("等待跳转到主页超时")
}
