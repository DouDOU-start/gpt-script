package register

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"chatgpt-register-script/internal/automation"
)

// 工作空间相关常量
const (
	workspaceLoadTimeout = 8 * time.Second        // 工作空间加载超时
	checkInterval        = 500 * time.Millisecond // 检查间隔
	menuWaitTimeout      = 1 * time.Second        // 菜单出现超时
	maxGetWorkspaceRetry = 3                      // 获取工作空间列表最大重试次数
)

// WorkspaceInfo 工作空间信息
type WorkspaceInfo struct {
	Name       string `json:"name"`
	IsPersonal bool   `json:"isPersonal"`
}

// WorkspaceManager 工作空间管理器
type WorkspaceManager struct {
	browser *automation.Browser
	taskID  string

	// 截图回调（可选）
	OnScreenshot func(step string)
}

// NewWorkspaceManager 创建工作空间管理器
func NewWorkspaceManager(browser *automation.Browser, taskID string) *WorkspaceManager {
	return &WorkspaceManager{
		browser: browser,
		taskID:  taskID,
	}
}

// WaitForWorkspaceLoad 等待工作空间加载完成，返回是否成功
func (m *WorkspaceManager) WaitForWorkspaceLoad() bool {
	if m.browser == nil {
		return false
	}

	// 先检查是否跳转到了 refresh_account 页面
	currentURL, err := m.browser.GetURL()
	if err != nil {
		log.Printf("[%s] 获取当前 URL 失败: %v", m.taskID, err)
	}
	if strings.Contains(currentURL, "refresh_account=true") {
		log.Printf("[%s] 检测到账号刷新页面，主动刷新到主页...", m.taskID)
		_ = m.browser.Navigate("https://chatgpt.com/")
		RandomDelay(2000, 3000)
	}

	// 检测页面是否加载完成（不再显示加载状态）
	checkScript := `() => {
		// 检测是否有加载指示器
		const loading = document.querySelector('[data-testid="loading"]');
		if (loading) return false;

		// 检测是否有 spinner
		const spinner = document.querySelector('.animate-spin');
		if (spinner) return false;

		// 检测主内容是否已加载
		const main = document.querySelector('main');
		if (!main) return false;

		// 检测聊天输入框是否存在（页面完全加载的标志）
		const textarea = document.querySelector('textarea');
		if (textarea) return true;

		// 检测工作空间选择器是否可用
		const wsSelector = document.querySelector('[data-testid="accounts-profile-button"]');
		if (wsSelector) return true;

		return false;
	}`

	maxChecks := int(workspaceLoadTimeout / checkInterval)
	for i := 0; i < maxChecks; i++ {
		var loaded bool
		if err := m.browser.Eval(checkScript, &loaded); err == nil && loaded {
			return true
		}
		time.Sleep(checkInterval)
	}
	return false
}

// GetAllWorkspaces 通过 JS 获取所有工作空间列表
// 使用多种方式尝试打开菜单，读取后自动关闭
func (m *WorkspaceManager) GetAllWorkspaces() []WorkspaceInfo {
	if m.browser == nil {
		return nil
	}

	// 先等待页面完全加载
	m.WaitForWorkspaceLoad()

	// 通过 JS 打开菜单、读取工作空间列表、关闭菜单
	script := `async () => {
		// 1. 等待菜单按钮出现并可点击
		let menuButton = null;
		for (let i = 0; i < 40; i++) {
			menuButton = document.querySelector('[data-testid="accounts-profile-button"]');
			if (menuButton && !menuButton.disabled) break;
			await new Promise(r => setTimeout(r, 100));
		}
		if (!menuButton) return { error: 'Menu button not found' };

		// 2. 尝试多种方式打开菜单
		let menu = null;

		// 方式1: 直接点击
		menuButton.click();
		for (let i = 0; i < 20; i++) {
			await new Promise(r => setTimeout(r, 50));
			menu = document.querySelector('[role="menu"]');
			if (menu) break;
		}

		// 方式2: 如果点击无效，尝试 focus + Enter
		if (!menu) {
			menuButton.focus();
			menuButton.dispatchEvent(new KeyboardEvent('keydown', {
				key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true
			}));
			for (let i = 0; i < 20; i++) {
				await new Promise(r => setTimeout(r, 50));
				menu = document.querySelector('[role="menu"]');
				if (menu) break;
			}
		}

		// 方式3: 如果还是没有菜单，尝试触发 mousedown + mouseup
		if (!menu) {
			menuButton.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
			menuButton.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
			for (let i = 0; i < 20; i++) {
				await new Promise(r => setTimeout(r, 50));
				menu = document.querySelector('[role="menu"]');
				if (menu) break;
			}
		}

		if (!menu) return { error: 'Menu did not appear' };

		// 3. 读取工作空间列表
		const workspaces = [];
		menu.querySelectorAll('[role="menuitemradio"]').forEach(item => {
			const text = item.textContent || '';
			const lowerText = text.toLowerCase();
			const isPersonal = lowerText.includes('personal account') ||
				lowerText.includes('personal workspace') ||
				lowerText.includes('personal') ||
				(lowerText.includes('chatgpt') && !lowerText.includes('team') &&
				 !lowerText.includes('business') && !lowerText.includes('enterprise'));

			let name = '';
			if (isPersonal) {
				name = 'Personal account';
			} else {
				for (const div of item.querySelectorAll('div')) {
					const t = div.textContent?.trim();
					if (t && !t.includes('member') && t.length < 50 && t.length > 0) {
						name = t;
						break;
					}
				}
			}

			if (name) {
				workspaces.push({ name, isPersonal });
			}
		});

		// 4. 关闭菜单
		document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

		return { success: true, workspaces };
	}`

	var result map[string]interface{}
	if err := m.browser.Eval(script, &result); err != nil {
		log.Printf("[%s] 获取工作空间列表失败: %v", m.taskID, err)
		return nil
	}

	if errMsg, ok := result["error"].(string); ok {
		log.Printf("[%s] 获取工作空间列表失败: %s", m.taskID, errMsg)
		return nil
	}

	workspacesRaw, ok := result["workspaces"].([]interface{})
	if !ok {
		return nil
	}

	var workspaces []WorkspaceInfo
	for _, ws := range workspacesRaw {
		wsMap, ok := ws.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := wsMap["name"].(string)
		isPersonal, _ := wsMap["isPersonal"].(bool)
		if name != "" {
			workspaces = append(workspaces, WorkspaceInfo{
				Name:       name,
				IsPersonal: isPersonal,
			})
		}
	}

	log.Printf("[%s] 获取到 %d 个工作空间: %v", m.taskID, len(workspaces), workspaces)
	return workspaces
}

// SwitchToWorkspaceByName 通过名称切换到指定工作空间
func (m *WorkspaceManager) SwitchToWorkspaceByName(name string) bool {
	if m.browser == nil {
		return false
	}

	// 使用 JSON 序列化安全传递参数，防止 JS 注入
	nameJSON, err := json.Marshal(name)
	if err != nil {
		log.Printf("[%s] 序列化工作空间名称失败: %v", m.taskID, err)
		return false
	}

	// 先等待页面完全加载，确保菜单按钮可用
	m.WaitForWorkspaceLoad()

	// 通过 JS 打开菜单并点击指定工作空间
	script := fmt.Sprintf(`async () => {
		const targetName = %s;

		// 1. 等待菜单按钮出现并可点击
		let menuButton = null;
		for (let i = 0; i < 40; i++) {
			menuButton = document.querySelector('[data-testid="accounts-profile-button"]');
			if (menuButton && !menuButton.disabled) break;
			await new Promise(r => setTimeout(r, 100));
		}
		if (!menuButton) return { error: 'Menu button not found' };

		// 2. 尝试多种方式打开菜单
		let menu = null;

		// 方式1: 直接点击
		menuButton.click();
		for (let i = 0; i < 20; i++) {
			await new Promise(r => setTimeout(r, 50));
			menu = document.querySelector('[role="menu"]');
			if (menu) break;
		}

		// 方式2: 如果点击无效，尝试 focus + Enter
		if (!menu) {
			menuButton.focus();
			menuButton.dispatchEvent(new KeyboardEvent('keydown', {
				key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true
			}));
			for (let i = 0; i < 20; i++) {
				await new Promise(r => setTimeout(r, 50));
				menu = document.querySelector('[role="menu"]');
				if (menu) break;
			}
		}

		// 方式3: 如果还是没有菜单，尝试触发 mousedown + mouseup
		if (!menu) {
			menuButton.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
			menuButton.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
			for (let i = 0; i < 20; i++) {
				await new Promise(r => setTimeout(r, 50));
				menu = document.querySelector('[role="menu"]');
				if (menu) break;
			}
		}

		if (!menu) return { error: 'Menu did not appear' };

		// 3. 查找并点击目标工作空间
		const items = menu.querySelectorAll('[role="menuitemradio"]');
		const allTexts = [];
		let personalItem = null;

		for (const item of items) {
			const text = item.textContent || '';
			allTexts.push(text.substring(0, 100));

			// 精确匹配：文本包含目标名称
			if (text.includes(targetName)) {
				item.click();
				return { success: true, clicked: targetName };
			}

			// 个人空间的模糊匹配（ChatGPT UI 在不同上下文中显示不同文本）
			if (targetName === 'Personal account') {
				const lowerText = text.toLowerCase();
				if (lowerText.includes('personal account') ||
					lowerText.includes('personal workspace') ||
					lowerText.includes('personal') ||
					(lowerText.includes('chatgpt') && !lowerText.includes('team') &&
					 !lowerText.includes('business') && !lowerText.includes('enterprise'))) {
					personalItem = item;
				}
			}
		}

		// 模糊匹配命中
		if (personalItem) {
			personalItem.click();
			return { success: true, clicked: targetName, method: 'fuzzy' };
		}

		// 最后一招：只有 2 个菜单项时，通过 aria-checked 反向推断
		if (targetName === 'Personal account' && items.length === 2) {
			for (const item of items) {
				if (item.getAttribute('aria-checked') === 'false') {
					item.click();
					return { success: true, clicked: targetName, method: 'inverse' };
				}
			}
		}

		// 未找到，关闭菜单并返回调试信息
		document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
		return { error: 'Workspace not found: ' + targetName, menuItems: allTexts, itemCount: items.length };
	}`, string(nameJSON))

	var result map[string]interface{}
	if err := m.browser.Eval(script, &result); err != nil {
		log.Printf("[%s] 切换工作空间失败: %v", m.taskID, err)
		return false
	}

	if errMsg, ok := result["error"].(string); ok {
		// 输出调试信息：菜单中所有项的文本
		if menuItems, ok := result["menuItems"].([]interface{}); ok {
			texts := make([]string, 0, len(menuItems))
			for _, item := range menuItems {
				if s, ok := item.(string); ok {
					texts = append(texts, s)
				}
			}
			log.Printf("[%s] 菜单项文本列表(%d项): %v", m.taskID, len(texts), texts)
		}
		log.Printf("[%s] 切换工作空间失败: %s", m.taskID, errMsg)
		return false
	}

	method, _ := result["method"].(string)
	if method != "" {
		log.Printf("[%s] 已切换到工作空间: %s (匹配方式: %s)", m.taskID, name, method)
	} else {
		log.Printf("[%s] 已切换到工作空间: %s", m.taskID, name)
	}

	// 等待页面加载
	RandomDelay(2000, 3000)
	m.HandleWorkspaceSetupDialogs()
	m.WaitForWorkspaceLoad()

	return true
}

// HandleWorkspaceSelectionDialog 处理登录后的工作空间选择对话框
// 返回：是否存在对话框，选择的工作空间名称
func (m *WorkspaceManager) HandleWorkspaceSelectionDialog() (bool, string) {
	if m.browser == nil {
		return false, ""
	}

	// 检测是否存在 "Launch a workspace" 对话框
	checkScript := `() => {
		const dialog = document.querySelector('dialog, [role="dialog"]');
		if (!dialog) return { exists: false };

		const heading = dialog.querySelector('h1, h2, [role="heading"]');
		if (!heading) return { exists: false };

		const text = heading.textContent || '';
		// 支持多种对话框标题：
		// - "Launch a workspace"
		// - "Select a workspace"
		// - "Join and launch your workspaces"
		if (!text.includes('Launch') && !text.includes('Select a workspace') && !text.includes('Join and launch')) {
			return { exists: false };
		}

		// 收集所有工作空间信息
		const workspaces = [];
		const listItems = dialog.querySelectorAll('li, [role="listitem"]');
		for (const item of listItems) {
			const text = item.textContent || '';
			const isPersonal = text.includes('Personal workspace') || text.includes('Personal account');
			const divs = item.querySelectorAll('div');
			let name = '';
			for (const div of divs) {
				const t = div.textContent?.trim();
				if (t && !t.includes('member') && !t.includes('Personal workspace') && t.length < 50) {
					name = t;
					break;
				}
			}
			workspaces.push({ name, isPersonal });
		}
		return { exists: true, workspaces };
	}`

	var result map[string]interface{}
	if err := m.browser.Eval(checkScript, &result); err != nil {
		return false, ""
	}

	exists, _ := result["exists"].(bool)
	if !exists {
		return false, ""
	}

	log.Printf("[%s] 检测到工作空间选择对话框", m.taskID)
	m.screenshot("workspace_selection_dialog")

	// 优先选择团队工作空间
	teamName := m.selectTeamWorkspace()
	if teamName != "" {
		return true, teamName
	}

	// 如果没有团队空间，选择个人空间
	selectPersonalScript := `() => {
		const dialog = document.querySelector('dialog, [role="dialog"]');
		if (!dialog) return { found: false };

		const listItems = dialog.querySelectorAll('li, [role="listitem"]');
		for (const item of listItems) {
			const text = item.textContent || '';
			if (text.includes('Personal workspace') || text.includes('Personal account')) {
				const openBtn = item.querySelector('button');
				if (openBtn) {
					openBtn.click();
					return { found: true, name: 'Personal account' };
				}
			}
		}
		return { found: false };
	}`

	if err := m.browser.Eval(selectPersonalScript, &result); err == nil {
		if found, _ := result["found"].(bool); found {
			name, _ := result["name"].(string)
			log.Printf("[%s] 已选择个人工作空间", m.taskID)
			RandomDelay(2000, 3000)
			m.WaitForWorkspaceLoad()
			return true, name
		}
	}

	return true, ""
}

// selectTeamWorkspace 选择团队工作空间（非 Personal workspace）
// 返回团队名称，空字符串表示未找到团队空间
func (m *WorkspaceManager) selectTeamWorkspace() string {
	if m.browser == nil {
		return ""
	}

	// 新版页面结构：dialog "Launch a workspace" -> list -> listitem -> button "Open"
	selectScript := `() => {
		// 新版：查找 "Launch a workspace" 对话框中的列表项
		const dialog = document.querySelector('dialog, [role="dialog"]');
		if (dialog) {
			const listItems = dialog.querySelectorAll('li, [role="listitem"]');
			for (const item of listItems) {
				const text = item.textContent || '';
				// 排除 Personal workspace
				if (!text.includes('Personal workspace') && !text.includes('Personal account')) {
					// 查找团队名称
					let name = '';
					const divs = item.querySelectorAll('div');
					for (const div of divs) {
						const directText = Array.from(div.childNodes)
							.filter(n => n.nodeType === Node.TEXT_NODE)
							.map(n => n.textContent.trim())
							.join('');
						if (directText && !directText.includes('member') && directText.length > 0 && directText.length < 50) {
							name = directText;
							break;
						}
					}
					// 备用：使用 truncate 元素
					if (!name) {
						const truncateEl = item.querySelector('[class*="truncate"]');
						if (truncateEl) {
							name = truncateEl.textContent?.trim() || '';
							name = name.replace(/\d+\s*members?$/i, '').trim();
						}
					}
					// 点击 Open 按钮（排除 Request 按钮）
					const openBtn = item.querySelector('button');
					if (openBtn && name) {
						const btnText = openBtn.textContent?.trim().toLowerCase() || '';
						if (btnText === 'open') {
							openBtn.click();
							return { found: true, name: name };
						}
					}
				}
			}
		}

		// 旧版兼容：使用 radio 按钮选择
		const radios = document.querySelectorAll('[role="radio"]');
		for (const radio of radios) {
			const truncateDiv = radio.querySelector('.truncate');
			const name = truncateDiv?.textContent?.trim() || '';
			if (name && !name.includes('Personal account') && !name.includes('Personal')) {
				radio.click();
				return { found: true, name: name };
			}
		}
		return { found: false };
	}`

	var result map[string]interface{}
	if err := m.browser.Eval(selectScript, &result); err != nil {
		log.Printf("[%s] 选择团队工作空间失败: %v", m.taskID, err)
		return ""
	}

	found, _ := result["found"].(bool)
	if !found {
		log.Printf("[%s] 未找到团队工作空间", m.taskID)
		return ""
	}

	name, _ := result["name"].(string)
	log.Printf("[%s] 已选择团队工作空间: %s", m.taskID, name)

	// 等待切换完成
	RandomDelay(2000, 3000)

	// 处理可能出现的设置对话框（首次进入团队空间）
	m.HandleWorkspaceSetupDialogs()

	// 等待页面加载
	m.WaitForWorkspaceLoad()

	m.screenshot("switched_to_team_workspace_" + name)
	return name
}

// HandleWorkspaceSetupDialogs 处理工作空间设置过程中的各种对话框
func (m *WorkspaceManager) HandleWorkspaceSetupDialogs() {
	m.handleEmptyWorkspaceDialog()
	m.handleContinueButton()
	m.handleWelcomeDialog()
	m.handleRoleSelectionDialog()
	m.handleDisplayNameDialog()
}

// handleEmptyWorkspaceDialog 处理 "Your ChatGPT Business workspace is ready" 对话框
// 选择 "Start as empty workspace" 保持个人工作空间独立
func (m *WorkspaceManager) handleEmptyWorkspaceDialog() bool {
	script := `() => {
		const elements = document.querySelectorAll('*');
		for (const el of elements) {
			if (el.textContent.includes('Start as empty workspace') &&
				(!el.querySelector('*:not(br)') || el.childNodes.length <= 3)) {
				let clickable = el;
				while (clickable && clickable.tagName !== 'BODY') {
					if (clickable.onclick || clickable.getAttribute('role') === 'radio' ||
						clickable.classList.contains('cursor-pointer') ||
						window.getComputedStyle(clickable).cursor === 'pointer') {
						clickable.click();
						return true;
					}
					clickable = clickable.parentElement;
				}
				if (el.parentElement) {
					el.parentElement.click();
					return true;
				}
			}
		}
		// 备用方案：查找 radio 按钮
		const radios = document.querySelectorAll('input[type="radio"]');
		for (const radio of radios) {
			const label = radio.closest('label') || radio.parentElement;
			if (label && label.textContent.includes('Start as empty workspace')) {
				radio.click();
				return true;
			}
		}
		return false;
	}`

	var clicked bool
	if err := m.browser.Eval(script, &clicked); err == nil && clicked {
		log.Printf("[%s] 已选择: Start as empty workspace", m.taskID)
		RandomDelay(500, 1000)
		return true
	}
	return false
}

// handleContinueButton 点击 Continue 按钮
func (m *WorkspaceManager) handleContinueButton() bool {
	script := `() => {
		const buttons = document.querySelectorAll('button');
		for (const btn of buttons) {
			if (btn.textContent.trim() === 'Continue') {
				btn.click();
				return true;
			}
		}
		return false;
	}`

	var clicked bool
	if err := m.browser.Eval(script, &clicked); err == nil && clicked {
		log.Printf("[%s] 已点击 Continue", m.taskID)
		RandomDelay(2000, 3000)
		return true
	}
	return false
}

// handleWelcomeDialog 处理 "Welcome to your secure workspace" 对话框
func (m *WorkspaceManager) handleWelcomeDialog() bool {
	script := `() => {
		const buttons = document.querySelectorAll('button');
		for (const btn of buttons) {
			if (btn.textContent.includes("Okay, let's go")) {
				btn.click();
				return true;
			}
		}
		return false;
	}`

	var clicked bool
	if err := m.browser.Eval(script, &clicked); err == nil && clicked {
		log.Printf("[%s] 已点击: Okay, let's go", m.taskID)
		RandomDelay(1000, 2000)
		return true
	}
	return false
}

// handleRoleSelectionDialog 处理 "What's your primary role?" 对话框
func (m *WorkspaceManager) handleRoleSelectionDialog() bool {
	script := `() => {
		const buttons = document.querySelectorAll('button');
		for (const btn of buttons) {
			if (btn.textContent.trim() === 'Skip') {
				btn.click();
				return true;
			}
		}
		return false;
	}`

	var clicked bool
	if err := m.browser.Eval(script, &clicked); err == nil && clicked {
		log.Printf("[%s] 已跳过角色选择", m.taskID)
		RandomDelay(500, 1000)
		return true
	}
	return false
}

// handleDisplayNameDialog 处理 "Update your workspace display name" 对话框
func (m *WorkspaceManager) handleDisplayNameDialog() bool {
	script := `() => {
		// 检查是否存在该对话框
		const pageText = document.body.innerText;
		if (!pageText.includes('Update your workspace display name')) {
			return { found: false, reason: 'dialog not present' };
		}

		// 方案1: 查找所有包含 "Update later" 文本的可点击元素
		const allElements = document.querySelectorAll('*');
		for (const el of allElements) {
			const directText = Array.from(el.childNodes)
				.filter(n => n.nodeType === Node.TEXT_NODE)
				.map(n => n.textContent.trim())
				.join('');

			if (directText === 'Update later' || el.textContent.trim() === 'Update later') {
				let clickTarget = el;
				for (let i = 0; i < 5 && clickTarget; i++) {
					const style = window.getComputedStyle(clickTarget);
					const isClickable = clickTarget.onclick ||
						clickTarget.tagName === 'A' ||
						clickTarget.tagName === 'BUTTON' ||
						clickTarget.getAttribute('role') === 'button' ||
						clickTarget.getAttribute('role') === 'link' ||
						style.cursor === 'pointer';

					if (isClickable || clickTarget.tagName === 'A' || clickTarget.tagName === 'BUTTON') {
						clickTarget.click();
						return { found: true, clicked: 'Update later', method: 'text-match' };
					}
					clickTarget = clickTarget.parentElement;
				}
				el.click();
				return { found: true, clicked: 'Update later', method: 'direct-click' };
			}
		}

		// 方案2: 查找对话框内的链接或按钮
		const dialog = document.querySelector('dialog, [role="dialog"], [data-state="open"]');
		if (dialog) {
			const candidates = dialog.querySelectorAll('a, button, [role="button"], [role="link"], [class*="link"], [class*="btn"]');
			for (const c of candidates) {
				const text = c.textContent.trim().toLowerCase();
				if (text.includes('later') || text.includes('skip') || text.includes('cancel')) {
					c.click();
					return { found: true, clicked: c.textContent.trim(), method: 'dialog-search' };
				}
			}
		}

		return { found: false, reason: 'update later button not found' };
	}`

	var result map[string]interface{}
	if err := m.browser.Eval(script, &result); err == nil {
		if found, _ := result["found"].(bool); found {
			clickedText, _ := result["clicked"].(string)
			method, _ := result["method"].(string)
			log.Printf("[%s] 已跳过工作空间名称更新 (clicked: %s, method: %s)", m.taskID, clickedText, method)
			RandomDelay(1000, 2000)
			return true
		} else if reason, _ := result["reason"].(string); reason != "dialog not present" {
			log.Printf("[%s] 工作空间名称更新对话框处理失败: %s", m.taskID, reason)
		}
	}
	return false
}

// SwitchToPersonalByNavigation 通过直接导航到 chatgpt.com 切换到个人空间
// 这是菜单匹配失败时的兜底方案：ChatGPT 默认加载个人工作空间
func (m *WorkspaceManager) SwitchToPersonalByNavigation() bool {
	if m.browser == nil {
		return false
	}

	log.Printf("[%s] 尝试通过导航切换到个人工作空间...", m.taskID)

	// 直接导航到 chatgpt.com 根路径
	if err := m.browser.Navigate("https://chatgpt.com/"); err != nil {
		log.Printf("[%s] 导航到 chatgpt.com 失败: %v", m.taskID, err)
		return false
	}

	RandomDelay(3000, 4000)
	m.HandleWorkspaceSetupDialogs()

	if !m.WaitForWorkspaceLoad() {
		log.Printf("[%s] 导航后页面加载失败", m.taskID)
		return false
	}

	// 验证是否确实在个人空间（通过 session API 检查）
	verifyScript := `async () => {
		try {
			const resp = await fetch('https://chatgpt.com/api/auth/session');
			const data = await resp.json();
			if (!data.account) return { isPersonal: true };
			const planType = data.account.planType || '';
			return { isPersonal: planType !== 'team' && planType !== 'enterprise' };
		} catch(e) {
			return { error: e.message };
		}
	}`

	var result map[string]interface{}
	if err := m.browser.Eval(verifyScript, &result); err != nil {
		log.Printf("[%s] 验证个人空间失败: %v", m.taskID, err)
		return true // 导航本身可能已成功，让上层尝试提取 session
	}

	if isPersonal, ok := result["isPersonal"].(bool); ok && isPersonal {
		log.Printf("[%s] 已通过导航成功切换到个人工作空间", m.taskID)
		return true
	}

	// 导航后仍在团队空间，说明 ChatGPT 记住了上次的工作空间
	log.Printf("[%s] 导航后仍在团队空间，返回失败", m.taskID)
	return false
}

// screenshot 触发截图回调（如果已设置）
func (m *WorkspaceManager) screenshot(step string) {
	if m.OnScreenshot != nil {
		m.OnScreenshot(step)
	}
}
