package register

import (
	"fmt"
	"log"
	"strings"
	"time"

	"chatgpt-register-script/internal/automation"
)

// SessionInfo 从浏览器提取的会话信息
type SessionInfo struct {
	AccessToken  string  `json:"accessToken"`
	Expires      string  `json:"expires"`
	WorkspaceID  string  `json:"workspaceID"`
	OrgID        string  `json:"orgID"`
	PlanType     string  `json:"planType"`
	IsTeam       bool    `json:"isTeam"`
	Cookies      string  `json:"cookies"`
	DeviceID     string  `json:"deviceID"`
	TokenExpires *string `json:"tokenExpires"`
}

// SaveWorkspaceFunc 保存工作空间的回调函数类型
// 参数：工作空间名称、会话信息
type SaveWorkspaceFunc func(name string, session SessionInfo) error

// SessionExtractor 会话提取器
type SessionExtractor struct {
	browser *automation.Browser
	taskID  string

	// 截图回调（可选）
	OnScreenshot func(step string)
}

// NewSessionExtractor 创建会话提取器
func NewSessionExtractor(browser *automation.Browser, taskID string) *SessionExtractor {
	return &SessionExtractor{
		browser: browser,
		taskID:  taskID,
	}
}

// ExtractSessionInfo 提取当前页面的 session 信息
func (s *SessionExtractor) ExtractSessionInfo() (*SessionInfo, error) {
	if s.browser == nil {
		return nil, fmt.Errorf("browser not initialized")
	}

	// 调用 /api/auth/session 获取完整的 session 信息
	sessionScript := `async () => {
		try {
			const response = await fetch('https://chatgpt.com/api/auth/session');
			const data = await response.json();
			return {
				accessToken: data.accessToken,
				expires: data.expires,
				account: data.account
			};
		} catch (e) {
			return { error: e.message };
		}
	}`

	var sessionData map[string]interface{}
	if err := s.browser.Eval(sessionScript, &sessionData); err != nil {
		return nil, fmt.Errorf("获取 session 失败: %v", err)
	}

	if errMsg, ok := sessionData["error"].(string); ok {
		return nil, fmt.Errorf("session API 错误: %s", errMsg)
	}

	accessToken, _ := sessionData["accessToken"].(string)
	if accessToken == "" {
		return nil, fmt.Errorf("session 中未找到 accessToken")
	}

	// 解析 account 信息
	var workspaceID, orgID, planType string
	var isTeam bool

	accountInfo, ok := sessionData["account"].(map[string]interface{})
	if ok {
		// 有 account 信息（通常是团队空间）
		workspaceID, _ = accountInfo["id"].(string)
		orgID, _ = accountInfo["organizationId"].(string)
		planType, _ = accountInfo["planType"].(string)
		isTeam = planType == "team" || planType == "enterprise"
	} else {
		// 个人空间没有 account 信息，从 JWT 中解析 user_id
		log.Printf("[%s] session 无 account 字段，尝试从 JWT 解析用户信息", s.taskID)
		userID := automation.ParseUserIDFromJWT(accessToken)
		if userID == "" {
			log.Printf("[%s] 无法从 JWT 解析 user_id", s.taskID)
			return nil, fmt.Errorf("无法从 JWT 解析 user_id")
		}
		workspaceID = userID
		planType = "free"
		isTeam = false
	}

	if workspaceID == "" {
		return nil, fmt.Errorf("session 中未找到 workspaceID")
	}

	// 获取 cookies
	cookies := s.getCookiesString()

	// 获取 device_id
	deviceID := s.getDeviceID()

	// 解析过期时间
	var tokenExpires *string
	if expiresStr, ok := sessionData["expires"].(string); ok && expiresStr != "" {
		tokenExpires = &expiresStr
	}

	info := &SessionInfo{
		AccessToken:  accessToken,
		Expires:      stringFromMap(sessionData, "expires"),
		WorkspaceID:  workspaceID,
		OrgID:        orgID,
		PlanType:     planType,
		IsTeam:       isTeam,
		Cookies:      cookies,
		DeviceID:     deviceID,
		TokenExpires: tokenExpires,
	}

	log.Printf("[%s] 已提取 session: workspaceID=%s, orgID=%s, planType=%s, isTeam=%v",
		s.taskID, workspaceID, orgID, planType, isTeam)

	return info, nil
}

// SaveWorkspaceSessionWithName 获取并保存指定工作空间的 session
// name: 工作空间名称（个人空间传 "Personal account"，团队空间传实际名称）
// saveFn: 保存回调函数
func (s *SessionExtractor) SaveWorkspaceSessionWithName(name string, saveFn SaveWorkspaceFunc) error {
	session, err := s.ExtractSessionInfo()
	if err != nil {
		return fmt.Errorf("提取 session 失败: %v", err)
	}

	if saveFn != nil {
		if err := saveFn(name, *session); err != nil {
			return fmt.Errorf("保存工作空间失败: %v", err)
		}
	}

	log.Printf("[%s] 已保存工作空间: name=%s, workspaceID=%s, orgID=%s, planType=%s, isTeam=%v",
		s.taskID, name, session.WorkspaceID, session.OrgID, session.PlanType, session.IsTeam)
	s.screenshot("saved_workspace_" + name)

	return nil
}

// SaveAllWorkspaces 遍历所有工作空间并保存 session
// saveFn: 保存回调函数，每个工作空间调用一次
func (s *SessionExtractor) SaveAllWorkspaces(wm *WorkspaceManager, saveFn SaveWorkspaceFunc) {
	if s.browser == nil {
		return
	}

	// 等待 session 数据准备就绪
	RandomDelay(2000, 3000)

	// 检测并处理工作空间选择对话框（新版登录流程）
	if hasDialog, selectedName := wm.HandleWorkspaceSelectionDialog(); hasDialog {
		// 新版流程：登录后需要先选择工作空间
		wm.WaitForWorkspaceLoad()
		wm.HandleWorkspaceSetupDialogs()
		RandomDelay(1000, 2000)

		if selectedName != "" {
			if err := s.SaveWorkspaceSessionWithName(selectedName, saveFn); err != nil {
				log.Printf("[%s] 保存工作空间 session 失败: %v", s.taskID, err)
			} else {
				log.Printf("[%s] 已保存工作空间 session: %s", s.taskID, selectedName)
			}
		}
		// 继续获取并保存其他工作空间
	}

	// 再次处理可能残留的对话框（确保页面可交互）
	wm.HandleWorkspaceSetupDialogs()
	RandomDelay(500, 1000)

	// 获取所有工作空间列表（最多重试 maxGetWorkspaceRetry 次）
	var workspaces []WorkspaceInfo
	for attempt := 1; attempt <= maxGetWorkspaceRetry; attempt++ {
		workspaces = wm.GetAllWorkspaces()
		if len(workspaces) > 0 {
			break
		}
		log.Printf("[%s] 获取工作空间列表失败，第 %d 次重试", s.taskID, attempt)
		if attempt < maxGetWorkspaceRetry {
			RandomDelay(2000, 3000)
		}
	}

	if len(workspaces) == 0 {
		log.Printf("[%s] 未获取到工作空间列表，尝试保存当前工作空间", s.taskID)
		s.screenshot("未获取到工作空间列表")
		// 降级：保存当前工作空间（使用统一的个人工作空间名称）
		if err := s.SaveWorkspaceSessionWithName("Personal account", saveFn); err != nil {
			log.Printf("[%s] 保存个人工作空间 session 失败: %v", s.taskID, err)
			s.screenshot("保存个人工作空间失败")
		}
		return
	}

	log.Printf("[%s] 开始保存 %d 个工作空间的 session", s.taskID, len(workspaces))

	// 调整顺序：个人空间优先（通常是当前所在空间，无需切换）
	sorted := make([]WorkspaceInfo, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws.IsPersonal {
			sorted = append([]WorkspaceInfo{ws}, sorted...)
		} else {
			sorted = append(sorted, ws)
		}
	}

	// 记录已保存的工作空间（避免重复）
	savedNames := make(map[string]bool)

	// 先尝试直接保存当前工作空间的 session（无需切换）
	currentSession, err := s.ExtractSessionInfo()
	if err == nil && currentSession != nil {
		for _, ws := range sorted {
			if (ws.IsPersonal && !currentSession.IsTeam) || (!ws.IsPersonal && currentSession.IsTeam) {
				if err := saveFn(ws.Name, *currentSession); err == nil {
					log.Printf("[%s] 已保存当前工作空间（无需切换）: %s", s.taskID, ws.Name)
					savedNames[ws.Name] = true
				}
				break
			}
		}
	}

	// 遍历处理剩余未保存的工作空间
	for i, ws := range sorted {
		if savedNames[ws.Name] {
			log.Printf("[%s] 工作空间 [%d/%d] %s 已保存，跳过", s.taskID, i+1, len(sorted), ws.Name)
			continue
		}

		log.Printf("[%s] 正在处理工作空间 [%d/%d]: %s", s.taskID, i+1, len(sorted), ws.Name)

		// 切换到该工作空间
		if !wm.SwitchToWorkspaceByName(ws.Name) {
			// 个人空间切换失败时使用导航兜底
			if ws.IsPersonal {
				log.Printf("[%s] 菜单切换个人空间失败，尝试导航兜底", s.taskID)
				if !wm.SwitchToPersonalByNavigation() {
					log.Printf("[%s] 导航兜底也失败，跳过工作空间 %s", s.taskID, ws.Name)
					continue
				}
			} else {
				log.Printf("[%s] 切换到工作空间 %s 失败，跳过", s.taskID, ws.Name)
				continue
			}
		}

		// 等待页面稳定
		RandomDelay(1000, 2000)

		// 保存该工作空间的 session（失败时重试最多 3 次）
		const maxSaveRetry = 3
		var saveErr error
		for attempt := 1; attempt <= maxSaveRetry; attempt++ {
			if saveErr = s.SaveWorkspaceSessionWithName(ws.Name, saveFn); saveErr == nil {
				log.Printf("[%s] 已保存工作空间 %s 的 session", s.taskID, ws.Name)
				break
			}
			log.Printf("[%s] 保存工作空间 %s 的 session 失败 (第 %d 次): %v", s.taskID, ws.Name, attempt, saveErr)
			if attempt < maxSaveRetry {
				RandomDelay(1500, 3000)
			}
		}
		if saveErr != nil {
			log.Printf("[%s] 工作空间 %s 的 session 保存最终失败，跳过", s.taskID, ws.Name)
		}
	}

	log.Printf("[%s] 所有工作空间 session 保存完成", s.taskID)
}

// getCookiesString 获取完整的 cookie 字符串
func (s *SessionExtractor) getCookiesString() string {
	script := `() => document.cookie`
	var cookies string
	if err := s.browser.Eval(script, &cookies); err != nil {
		log.Printf("[%s] 获取 cookies 失败: %v", s.taskID, err)
		return ""
	}
	return cookies
}

// getDeviceID 从 cookies 提取 oai-did（设备 ID）
func (s *SessionExtractor) getDeviceID() string {
	script := `() => document.cookie.match(/oai-did=([^;]+)/)?.[1]`
	var deviceID string
	if err := s.browser.Eval(script, &deviceID); err != nil {
		log.Printf("[%s] 获取 device_id 失败: %v", s.taskID, err)
		return ""
	}
	return deviceID
}

// CheckAccountStatus 检查页面是否显示账号被禁用的错误
// 返回错误信息，nil 表示账号正常
func (s *SessionExtractor) CheckAccountStatus() error {
	var pageText string
	_ = s.browser.Eval(`() => document.body.innerText`, &pageText)

	if strings.Contains(pageText, "deleted or deactivated") ||
		strings.Contains(pageText, "account_deactivated") ||
		(strings.Contains(pageText, "error occurred") && strings.Contains(pageText, "do not have an account")) {
		return fmt.Errorf("账号已被 OpenAI 停用或删除")
	}
	return nil
}

// WaitForSessionReady 等待 session 准备就绪
func (s *SessionExtractor) WaitForSessionReady(timeout time.Duration) bool {
	deadline := time.Now().UTC().Add(timeout)
	for time.Now().UTC().Before(deadline) {
		session, err := s.ExtractSessionInfo()
		if err == nil && session.AccessToken != "" {
			return true
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

// screenshot 触发截图回调（如果已设置）
func (s *SessionExtractor) screenshot(step string) {
	if s.OnScreenshot != nil {
		s.OnScreenshot(step)
	}
}

// stringFromMap 安全地从 map 中获取字符串
func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
