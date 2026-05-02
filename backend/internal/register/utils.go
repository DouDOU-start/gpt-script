package register

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"chatgpt-register-script/internal/automation"
)

// 常用英文名字库
var firstNames = []string{
	"James", "John", "Robert", "Michael", "David", "William", "Richard", "Joseph", "Thomas", "Christopher",
	"Mary", "Patricia", "Jennifer", "Linda", "Elizabeth", "Barbara", "Susan", "Jessica", "Sarah", "Karen",
	"Daniel", "Matthew", "Anthony", "Mark", "Donald", "Steven", "Paul", "Andrew", "Joshua", "Kenneth",
	"Nancy", "Betty", "Margaret", "Sandra", "Ashley", "Dorothy", "Kimberly", "Emily", "Donna", "Michelle",
	"Kevin", "Brian", "George", "Timothy", "Ronald", "Edward", "Jason", "Jeffrey", "Ryan", "Jacob",
	"Carol", "Amanda", "Melissa", "Deborah", "Stephanie", "Rebecca", "Sharon", "Laura", "Cynthia", "Kathleen",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez",
	"Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin",
	"Lee", "Perez", "Thompson", "White", "Harris", "Sanchez", "Clark", "Ramirez", "Lewis", "Robinson",
	"Walker", "Young", "Allen", "King", "Wright", "Scott", "Torres", "Nguyen", "Hill", "Flores",
	"Green", "Adams", "Nelson", "Baker", "Hall", "Rivera", "Campbell", "Mitchell", "Carter", "Roberts",
}

// verificationCodeSelectors 验证码输入框的多种可能选择器
var verificationCodeSelectors = []string{
	`input[name="code"]`,
	`input[type="text"][inputmode="numeric"]`,
	`input[autocomplete="one-time-code"]`,
}

const verificationDigitSelector = `input[inputmode="numeric"], input[autocomplete="one-time-code"], input[aria-label*="code" i], input[maxlength="1"]`

type RegisterPageState string

const (
	RegisterStateUnknown      RegisterPageState = "unknown"
	RegisterStateLanding      RegisterPageState = "landing"
	RegisterStateEmail        RegisterPageState = "email"
	RegisterStatePhone        RegisterPageState = "phone"
	RegisterStatePassword     RegisterPageState = "password"
	RegisterStateVerification RegisterPageState = "verification"
	RegisterStatePersonalInfo RegisterPageState = "personal_info"
	RegisterStateMain         RegisterPageState = "main"
	RegisterStateWorkspace    RegisterPageState = "workspace"
	RegisterStateEmailExists  RegisterPageState = "email_exists"
	RegisterStateError        RegisterPageState = "error"
)

// AccountBannedError 账号被禁错误
type AccountBannedError struct {
	Email string
}

func (e *AccountBannedError) Error() string {
	return fmt.Sprintf("账号 %s 已被 OpenAI 停用或删除", e.Email)
}

// IsAccountBannedError 判断是否为账号被禁错误
func IsAccountBannedError(err error) bool {
	_, ok := err.(*AccountBannedError)
	return ok
}

// GenerateRandomName 生成随机英文姓名
func GenerateRandomName() string {
	firstName := firstNames[rand.Intn(len(firstNames))]
	lastName := lastNames[rand.Intn(len(lastNames))]
	return firstName + " " + lastName
}

// GenerateRandomBirthday 生成随机生日（确保至少 19 岁，范围 1970-2006 年）
func GenerateRandomBirthday() Birthday {
	year := 1970 + rand.Intn(37) // 1970-2006
	month := 1 + rand.Intn(12)   // 1-12
	day := 1 + rand.Intn(28)     // 1-28 避免月份天数问题
	return Birthday{
		Year:  fmt.Sprintf("%d", year),
		Month: fmt.Sprintf("%02d", month),
		Day:   fmt.Sprintf("%02d", day),
	}
}

// RandomDelay 随机延迟（毫秒）
func RandomDelay(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	d := rand.Intn(maxMs-minMs) + minMs
	time.Sleep(time.Duration(d) * time.Millisecond)
}

func anyVisible(b *automation.Browser, selectors ...string) bool {
	var ok bool
	raw, _ := json.Marshal(selectors)
	script := fmt.Sprintf(`() => {
		const selectors = %s;
		const visible = el => {
			const r = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return r.width > 0 && r.height > 0 && style.visibility !== 'hidden' && style.display !== 'none' && !el.disabled;
		};
		for (const selector of selectors) {
			try {
				if (Array.from(document.querySelectorAll(selector)).some(visible)) return true;
			} catch (_) {}
		}
		return false;
	}`, raw)
	_ = b.Eval(script, &ok)
	return ok
}

// IsVerificationCodeVisible 检测验证码输入框是否可见
func IsVerificationCodeVisible(b *automation.Browser) bool {
	return anyVisible(b, verificationCodeSelectors...) || visibleVerificationDigitCount(b) >= 6
}

// isCloudflareChallenge 检测是否被 Cloudflare 人机验证拦截
func isCloudflareChallenge(b *automation.Browser) bool {
	title, err := b.GetTitle()
	if err == nil && title == "Just a moment..." {
		return true
	}
	if anyVisible(b, `iframe[src*="challenges.cloudflare.com"]`, `input[value="Verify you are human"]`) {
		return true
	}
	var text string
	_ = b.Eval(`() => document.body ? document.body.innerText : ''`, &text)
	return strings.Contains(text, "Verify you are human")
}

// getVerificationCodeSelector 获取当前可见的验证码输入框选择器
func getVerificationCodeSelector(b *automation.Browser) string {
	for _, sel := range verificationCodeSelectors {
		if b.IsVisible(sel) {
			return sel
		}
	}
	if visibleVerificationDigitCount(b) >= 6 {
		return verificationDigitSelector
	}
	return verificationCodeSelectors[0]
}

func visibleVerificationDigitCount(b *automation.Browser) int {
	var count int
	_ = b.Eval(`() => {
		const visible = el => {
			const r = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return r.width > 0 && r.height > 0 && style.visibility !== 'hidden' && style.display !== 'none' && !el.disabled;
		};
		return Array.from(document.querySelectorAll('input[inputmode="numeric"], input[autocomplete="one-time-code"], input[aria-label*="code" i], input[maxlength="1"]'))
			.filter(visible).length;
	}`, &count)
	return count
}

// clickContinueButton 点击 Continue 按钮
func clickContinueButton(b *automation.Browser) error {
	script := `() => {
		const candidates = ['Continue', '继续', 'Next', '下一步'];
		const btns = Array.from(document.querySelectorAll('button'));
		for (const t of candidates) {
			const btn = btns.find(b => (b.textContent || '').trim() === t && !b.disabled);
			if (btn) { btn.click(); return true; }
		}
		const submit = btns.find(b => b.type === 'submit' && !b.disabled);
		if (submit) { submit.click(); return true; }
		return false;
	}`

	var clicked bool
	if err := b.Eval(script, &clicked); err != nil {
		return fmt.Errorf("点击 Continue 按钮失败: %w", err)
	}
	if !clicked {
		return fmt.Errorf("未找到 Continue 按钮")
	}
	return nil
}

// isLikelyOnChatGPTMainPage 判断是否在 ChatGPT 主页
func isLikelyOnChatGPTMainPage(url string) bool {
	u := strings.ToLower(strings.TrimSpace(url))
	if !strings.Contains(u, "chatgpt.com") {
		return false
	}
	if strings.Contains(u, "/auth") || strings.Contains(u, "auth.openai.com") || strings.Contains(u, "login.openai.com") {
		return false
	}
	return true
}

func detectRegisterState(b *automation.Browser, email string) RegisterPageState {
	currentURL, _ := b.GetURL()
	lowerURL := strings.ToLower(currentURL)
	if strings.Contains(lowerURL, "auth.openai.com/workspace") {
		return RegisterStateWorkspace
	}
	if IsVerificationCodeVisible(b) || strings.Contains(lowerURL, "email-verification") || strings.Contains(lowerURL, "verify-email") {
		return RegisterStateVerification
	}
	if isPersonalInfoVisible(b) {
		return RegisterStatePersonalInfo
	}
	if anyVisible(b, `input[type="password"]`) {
		return RegisterStatePassword
	}

	info := getRegisterPageInfo(b)
	body := strings.ToLower(info.Text)
	if strings.Contains(body, "already exists") || strings.Contains(body, "already have an account") || strings.Contains(body, "user_already_exists") || strings.Contains(body, "email already") {
		return RegisterStateEmailExists
	}
	if strings.Contains(body, "something went wrong") || strings.Contains(body, "try again later") || strings.Contains(body, "request timed out") || strings.Contains(body, "too many attempts") || strings.Contains(body, "unable to") {
		return RegisterStateError
	}
	if anyVisible(b, `input[name="email"]`, `input[type="email"]`) {
		return RegisterStateEmail
	}
	if anyVisible(b, `input[type="tel"]`) || strings.Contains(body, "phone number") || strings.Contains(body, "手机号") {
		return RegisterStatePhone
	}
	if info.HasSignupButton || info.HasLoginButton || strings.Contains(lowerURL, "auth.openai.com") {
		return RegisterStateLanding
	}
	if isLikelyOnChatGPTMainPage(currentURL) && hasChatGPTMainFeature(b) {
		return RegisterStateMain
	}
	if strings.Contains(lowerURL, "chatgpt.com") || strings.TrimSpace(email) != "" {
		return RegisterStateLanding
	}
	return RegisterStateUnknown
}

func getRegisterPageInfo(b *automation.Browser) registerPageInfo {
	var info registerPageInfo
	_ = b.Eval(`() => {
		const text = document.body ? document.body.innerText : '';
		const buttons = Array.from(document.querySelectorAll('button, a, [role="button"]'));
		const visible = el => {
			const r = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return r.width > 0 && r.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
		};
		const hasText = values => buttons.some(el => {
			const t = (el.textContent || '').trim().toLowerCase();
			return visible(el) && values.some(v => t === v || t.includes(v));
		});
		return {
			text,
			hasSignupButton: hasText(['sign up', 'create account', 'get started', '注册']),
			hasLoginButton: hasText(['log in', 'login', '登录']),
		};
	}`, &info)
	return info
}

type registerPageInfo struct {
	Text            string `json:"text"`
	HasSignupButton bool   `json:"hasSignupButton"`
	HasLoginButton  bool   `json:"hasLoginButton"`
}

func isPersonalInfoVisible(b *automation.Browser) bool {
	return anyVisible(b,
		`input[name="name"]`,
		`[role="spinbutton"]`,
		`input[name="birthday"]`,
		`input[name="age"]`,
		`[data-testid="hidden-select-container"] select`,
	)
}

func hasChatGPTMainFeature(b *automation.Browser) bool {
	var ok bool
	_ = b.Eval(`() => {
		const text = document.body ? document.body.innerText : '';
		return document.querySelector('[data-testid="conversation"]') !== null ||
			document.querySelector('textarea') !== null ||
			text.includes('New chat') || text.includes('Message ChatGPT');
	}`, &ok)
	return ok
}

func pageShowsExpectedEmail(b *automation.Browser, email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return true
	}
	var hasEmail bool
	script := fmt.Sprintf(`() => {
		const email = %q;
		const text = (document.body ? document.body.innerText : '').toLowerCase();
		const inputs = Array.from(document.querySelectorAll('input')).map(i => (i.value || '').toLowerCase()).join('\n');
		return text.includes(email) || inputs.includes(email);
	}`, email)
	_ = b.Eval(script, &hasEmail)
	return hasEmail
}

func clickAuthEntryButton(b *automation.Browser, isLogin bool) (bool, error) {
	var clicked bool
	mode := "false"
	if isLogin {
		mode = "true"
	}
	script := fmt.Sprintf(`() => {
		const isLogin = %s;
		const signup = ['sign up', 'create account', 'get started', 'sign up for free', '注册'];
		const login = ['log in', 'login', '登录'];
		const candidates = isLogin ? login : signup;
		const elements = Array.from(document.querySelectorAll('button, a, [role="button"]'));
		const visible = el => {
			const r = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return r.width > 0 && r.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
		};
		const blockers = ['close', 'dismiss', 'skip'];
		const dialog = document.querySelector('[role="dialog"], [data-radix-dialog-content]');
		if (dialog) {
			const closeButton = Array.from(dialog.querySelectorAll('button, [role="button"]')).find(e => {
				const text = (e.textContent || '').trim().toLowerCase();
				const label = (e.getAttribute('aria-label') || '').trim().toLowerCase();
				return visible(e) && !e.disabled && (blockers.includes(text) || blockers.includes(label) || label.includes('close'));
			});
			if (closeButton) {
				closeButton.click();
				return true;
			}
		}
		const sorted = elements
			.filter(e => visible(e) && !e.disabled)
			.sort((a, b) => b.getBoundingClientRect().top - a.getBoundingClientRect().top);
		for (const target of candidates) {
			const el = sorted.find(e => {
				const text = (e.textContent || '').trim().toLowerCase();
				return text === target || text.includes(target);
			});
			if (el) {
				const href = el.href || el.getAttribute('href');
				if (href) {
					window.location.href = href;
					return true;
				}
				el.scrollIntoView({ block: 'center', inline: 'center' });
				const rect = el.getBoundingClientRect();
				el.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: rect.left + rect.width / 2, clientY: rect.top + rect.height / 2 }));
				el.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, clientX: rect.left + rect.width / 2, clientY: rect.top + rect.height / 2 }));
				el.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, clientX: rect.left + rect.width / 2, clientY: rect.top + rect.height / 2 }));
				el.click();
				return true;
			}
		}
		return false;
	}`, mode)
	if err := b.Eval(script, &clicked); err != nil {
		return false, err
	}
	return clicked, nil
}

func switchPhoneToEmail(b *automation.Browser) (bool, error) {
	var clicked bool
	script := `() => {
		const targets = ['use email', 'continue with email', 'email instead', 'sign up with email', 'use your email', '使用邮箱', '邮箱'];
		const elements = Array.from(document.querySelectorAll('button, a'));
		for (const target of targets) {
			const el = elements.find(e => {
				const text = (e.textContent || '').trim().toLowerCase();
				return !e.disabled && (text === target || text.includes(target));
			});
			if (el) { el.click(); return true; }
		}
		return false;
	}`
	if err := b.Eval(script, &clicked); err != nil {
		return false, err
	}
	return clicked, nil
}

func getAuthErrorText(b *automation.Browser) string {
	var text string
	_ = b.Eval(`() => {
		const nodes = Array.from(document.querySelectorAll('[role="alert"], [aria-live], [class*="error" i], [class*="invalid" i], p, span, div'));
		return nodes.map(n => (n.textContent || '').trim()).filter(Boolean).join('\n');
	}`, &text)
	return strings.TrimSpace(text)
}

// waitForAnyVisible 等待任意一个选择器可见
func waitForAnyVisible(b *automation.Browser, selectors []string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, sel := range selectors {
			if b.IsVisible(sel) {
				return sel, true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return "", false
}

// getStepStatusAndMessage 获取步骤对应的状态和消息
func (r *ChatGPTRegister) getStepStatusAndMessage(stepName string) (string, string) {
	stepMap := map[string]struct {
		status  string
		message string
	}{
		"setup":            {"setup", "初始化浏览器"},
		"navigate":         {"navigating", "访问注册页面"},
		"click_signup":     {"clicking_signup", "点击注册按钮"},
		"input_email":      {"entering_email", "输入邮箱地址"},
		"input_password":   {"entering_password", "输入密码"},
		"verify":           {"running", "等待邮箱验证"},
		"verify_if_needed": {"running", "检查是否需要验证"},
		"personal_info":    {"filling_info", "填写个人信息"},
	}

	if info, ok := stepMap[stepName]; ok {
		return info.status, info.message
	}
	return "running", "执行步骤: " + stepName
}

// sendStatus 发送状态通知
func (r *ChatGPTRegister) sendStatus(status, message string, data map[string]interface{}) {
	if r.statusCallback != nil {
		r.statusCallback(status, message, data)
	}
}

// sendError 发送错误通知
func (r *ChatGPTRegister) sendError(step, errMsg string) {
	r.sendStatus("error", errMsg, map[string]interface{}{
		"step":  step,
		"error": errMsg,
	})
}

// dumpPageHTML 步骤失败时记录页面 HTML（用于排查页面结构变化）
func (r *ChatGPTRegister) dumpPageHTML(stepName string) {
	if r.browser == nil {
		return
	}
	html, err := r.browser.GetHTML()
	if err != nil {
		log.Printf("[%s] 获取页面 HTML 失败: %v", r.taskID, err)
		return
	}
	// 截断过长的 HTML，避免日志爆炸
	const maxLen = 50000
	if len(html) > maxLen {
		html = html[:maxLen] + "\n... (truncated)"
	}
	log.Printf("[%s] 步骤 %s 失败时页面 HTML:\n%s", r.taskID, stepName, html)
}

// sendScreenshot 发送截图
func (r *ChatGPTRegister) sendScreenshot(desc string) {
	if r.browser == nil || r.screenshotCallback == nil {
		return
	}

	r.stepCount++

	png, err := r.browser.ScreenshotPNG()
	if err != nil {
		log.Printf("[%s] 截图失败: %s, %v", r.taskID, desc, err)
		return
	}

	r.screenshotCallback(desc, png)
}

// WorkspaceSession 工作空间 session 信息
type WorkspaceSession struct {
	WorkspaceID        string  `json:"workspace_id"`
	Name               *string `json:"name"`
	Structure          string  `json:"structure"`
	IsTeam             bool    `json:"is_team"`
	PlanType           string  `json:"plan_type"`
	SubscriptionStatus string  `json:"subscription_status"`
	WillRenew          bool    `json:"will_renew"`
	BillingCycleEnd    *string `json:"billing_cycle_end"`
	OrganizationID     *string `json:"organization_id"`
	SeatsUsed          int     `json:"seats_used"`
	SeatsTotal         int     `json:"seats_total"`
	AccessToken        *string `json:"access_token"`
	TokenExpires       *string `json:"token_expires"`
}

// WorkspacesToJSON 将工作空间列表序列化为 JSON
func WorkspacesToJSON(workspaces []WorkspaceSession) (string, error) {
	b, err := json.Marshal(workspaces)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WorkspacesFromJSON 从 JSON 反序列化工作空间列表
func WorkspacesFromJSON(raw string) ([]WorkspaceSession, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []WorkspaceSession
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// buildWorkspacesFromSession 从 session 构建工作空间 JSON
func buildWorkspacesFromSession(session automation.SessionLite) (string, error) {
	accessToken := strings.TrimSpace(session.AccessToken)
	if accessToken == "" {
		return "", fmt.Errorf("missing accessToken")
	}

	var workspaceID string
	var organizationID string
	planType := ""
	isTeam := false
	structure := "personal"

	if session.Account != nil && strings.TrimSpace(session.Account.ID) != "" {
		workspaceID = strings.TrimSpace(session.Account.ID)
		organizationID = strings.TrimSpace(session.Account.OrganizationID)
		planType = strings.TrimSpace(session.Account.PlanType)
		isTeam = planType == "team" || planType == "enterprise"
		structure = "workspace"
	} else {
		workspaceID = automation.ParseUserIDFromJWT(accessToken)
		if workspaceID == "" {
			return "", fmt.Errorf("missing workspace_id in session")
		}
		planType = "free"
		isTeam = false
		structure = "personal"
	}

	if strings.TrimSpace(planType) == "" {
		planType = "free"
	}

	tokenExpires := strPtrOrNil(strings.TrimSpace(session.Expires))
	orgPtr := strPtrOrNil(organizationID)

	ws := WorkspaceSession{
		WorkspaceID:        workspaceID,
		Name:               nil,
		Structure:          structure,
		IsTeam:             isTeam,
		PlanType:           planType,
		SubscriptionStatus: "none",
		WillRenew:          false,
		BillingCycleEnd:    nil,
		OrganizationID:     orgPtr,
		SeatsUsed:          0,
		SeatsTotal:         0,
		AccessToken:        &accessToken,
		TokenExpires:       tokenExpires,
	}

	b, err := json.Marshal([]WorkspaceSession{ws})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// strPtrOrNil 返回字符串指针，空字符串返回 nil
func strPtrOrNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// sessionInfoToWorkspaceSession 将 SessionInfo 转换为 WorkspaceSession
func sessionInfoToWorkspaceSession(name string, info SessionInfo) WorkspaceSession {
	structure := "personal"
	if info.IsTeam {
		structure = "workspace"
	}

	planType := info.PlanType
	if planType == "" {
		planType = "free"
	}

	accessToken := info.AccessToken

	return WorkspaceSession{
		WorkspaceID:        info.WorkspaceID,
		Name:               strPtrOrNil(name),
		Structure:          structure,
		IsTeam:             info.IsTeam,
		PlanType:           planType,
		SubscriptionStatus: "none",
		OrganizationID:     strPtrOrNil(info.OrgID),
		AccessToken:        &accessToken,
		TokenExpires:       info.TokenExpires,
	}
}
