package team

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	seatUsageBased = "usage_based"
	seatDefault    = "default"
	memberPageSize = 50
)

var teamPlanPrefixes = []string{"team", "team/", "business/", "self_serve_business_"}
var personalPlanTypes = map[string]bool{"free": true, "plus": true, "pro": true, "personal/free": true, "personal/plus": true, "personal/pro": true}

type Invite struct {
	Email    string `json:"email"`
	SeatType string `json:"seat_type"`
	Note     string `json:"note,omitempty"`
}

type InviteResult struct {
	Invite       Invite   `json:"invite"`
	Success      bool     `json:"success"`
	Errors       []string `json:"errors,omitempty"`
	StatusCode   int      `json:"status_code,omitempty"`
	ResponseBody string   `json:"response_body,omitempty"`
}

type ActionResult struct {
	Success      bool     `json:"success"`
	Errors       []string `json:"errors,omitempty"`
	StatusCode   int      `json:"status_code,omitempty"`
	RequestURL   string   `json:"request_url,omitempty"`
	ResponseBody string   `json:"response_body,omitempty"`
}

type CaptainTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	WorkspaceID  string `json:"workspace_id"`
}

func (t CaptainTokens) Complete() bool {
	return strings.TrimSpace(t.AccessToken) != "" && strings.TrimSpace(t.RefreshToken) != "" && strings.TrimSpace(t.WorkspaceID) != ""
}

type RefreshResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type AccountInfo struct {
	AllAccounts      []map[string]string `json:"all_accounts"`
	TeamAccounts     []map[string]string `json:"team_accounts"`
	PersonalAccounts []map[string]string `json:"personal_accounts"`
}

type WorkspaceService struct {
	Client      *http.Client
	BaseURL     string
	AuthBaseURL string
	Language    string
	deviceID    string
	sessionID   string
}

func NewWorkspaceService(baseURL, authBaseURL string) *WorkspaceService {
	return &WorkspaceService{Client: &http.Client{Timeout: 60 * time.Second}, BaseURL: strings.TrimRight(baseURL, "/"), AuthBaseURL: strings.TrimRight(authBaseURL, "/"), Language: "en-US"}
}

func ValidateSeatType(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != seatUsageBased && value != seatDefault {
		return "", fmt.Errorf("unsupported team seat_type: %q", value)
	}
	return value, nil
}

func (s *WorkspaceService) RefreshAccessToken(ctx context.Context, refreshToken string) (RefreshResult, error) {
	form := url.Values{}
	form.Set("client_id", "app_EMoamEEZ73f0CkXaXp7hrann")
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("redirect_uri", "http://localhost:1455/auth/callback")
	form.Set("audience", "https://api.openai.com/v1")
	res, body, err := s.do(ctx, http.MethodPost, s.AuthBaseURL+"/oauth/token", strings.NewReader(form.Encode()), map[string]string{"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return RefreshResult{}, err
	}
	if res.StatusCode >= 400 {
		return RefreshResult{}, fmt.Errorf("refresh access token failed: HTTP %d: %s", res.StatusCode, summarize(body))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{AccessToken: strings.TrimSpace(fmt.Sprint(payload["access_token"])), RefreshToken: firstNonEmpty(fmt.Sprint(payload["refresh_token"]), refreshToken)}, nil
}

func (s *WorkspaceService) GetAccountInfo(ctx context.Context, accessToken string) (AccountInfo, error) {
	res, body, err := s.do(ctx, http.MethodGet, s.BaseURL+"/backend-api/accounts/check/v4-2023-04-27", nil, s.authHeaders(accessToken))
	if err != nil {
		return AccountInfo{}, err
	}
	if res.StatusCode >= 400 {
		return AccountInfo{}, fmt.Errorf("get account info failed: HTTP %d: %s", res.StatusCode, summarize(body))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{}
	accounts, _ := payload["accounts"].(map[string]any)
	for workspaceID, raw := range accounts {
		item, _ := raw.(map[string]any)
		account, _ := item["account"].(map[string]any)
		entry := map[string]string{"account_id": workspaceID, "plan_type": fmt.Sprint(account["plan_type"]), "name": fmt.Sprint(account["name"])}
		info.AllAccounts = append(info.AllAccounts, entry)
		if IsTeamPlanType(entry["plan_type"]) {
			info.TeamAccounts = append(info.TeamAccounts, entry)
		}
		if IsPersonalPlanType(entry["plan_type"]) {
			info.PersonalAccounts = append(info.PersonalAccounts, entry)
		}
	}
	return info, nil
}

func (s *WorkspaceService) ListMembers(ctx context.Context, accessToken, workspaceID string) ([]map[string]any, error) {
	return s.listPaged(ctx, accessToken, workspaceID, "users", "/admin/members")
}

func (s *WorkspaceService) ListInvites(ctx context.Context, accessToken, workspaceID string) ([]map[string]any, error) {
	return s.listPaged(ctx, accessToken, workspaceID, "invites", "/admin/members?tab=invites")
}

func (s *WorkspaceService) FindMemberIDByEmail(ctx context.Context, accessToken, workspaceID, email string) (string, error) {
	members, err := s.ListMembers(ctx, accessToken, workspaceID)
	if err != nil {
		return "", err
	}
	expected := strings.ToLower(strings.TrimSpace(email))
	for _, item := range members {
		if strings.ToLower(strings.TrimSpace(fmt.Sprint(item["email"]))) == expected && strings.TrimSpace(fmt.Sprint(item["id"])) != "" {
			return strings.TrimSpace(fmt.Sprint(item["id"])), nil
		}
		user, _ := item["user"].(map[string]any)
		if strings.ToLower(strings.TrimSpace(fmt.Sprint(user["email"]))) == expected && strings.TrimSpace(fmt.Sprint(user["id"])) != "" {
			return strings.TrimSpace(fmt.Sprint(user["id"])), nil
		}
	}
	return "", nil
}

func (s *WorkspaceService) CancelInvite(ctx context.Context, accessToken, workspaceID, email string) ActionResult {
	body, _ := json.Marshal(map[string]string{"email_address": email})
	headers := s.workspaceHeaders(accessToken, workspaceID, "/backend-api/accounts/"+workspaceID+"/invites", "/backend-api/accounts/{account_id}/invites", "/admin/members?tab=invites")
	res, payload, err := s.do(ctx, http.MethodDelete, s.BaseURL+"/backend-api/accounts/"+workspaceID+"/invites", bytes.NewReader(body), headers)
	return buildActionResult(res, payload, err)
}

func (s *WorkspaceService) Invite(ctx context.Context, invite Invite, captain CaptainTokens) InviteResult {
	results := s.InviteMany(ctx, []Invite{invite}, captain)
	if len(results) == 0 {
		return InviteResult{Invite: invite, Errors: []string{"empty_invite_batch"}}
	}
	return results[0]
}

func (s *WorkspaceService) InviteMany(ctx context.Context, invites []Invite, captain CaptainTokens) []InviteResult {
	if len(invites) == 0 {
		return nil
	}
	seatType := invites[0].SeatType
	if _, err := ValidateSeatType(seatType); err != nil {
		return inviteErrors(invites, err.Error())
	}
	for _, invite := range invites[1:] {
		if invite.SeatType != seatType {
			return inviteErrors(invites, "team invites in a single batch must share the same seat_type")
		}
	}
	emails := []string{}
	for _, invite := range invites {
		emails = append(emails, invite.Email)
	}
	body, _ := json.Marshal(map[string]any{"email_addresses": emails, "role": "standard-user", "seat_type": seatType, "resend_emails": true})
	headers := s.workspaceHeaders(captain.AccessToken, captain.WorkspaceID, "/backend-api/accounts/"+captain.WorkspaceID+"/invites", "/backend-api/accounts/{account_id}/invites", "/admin/members")
	res, payload, err := s.do(ctx, http.MethodPost, s.BaseURL+"/backend-api/accounts/"+captain.WorkspaceID+"/invites", bytes.NewReader(body), headers)
	success := err == nil && res.StatusCode >= 200 && res.StatusCode < 300
	errors := []string{}
	statusCode := 0
	responseBody := ""
	if !success {
		statusCode = statusOf(res)
		responseBody = summarize(payload)
		errors = extractErrors(statusCode, payload, err)
	}
	out := []InviteResult{}
	for _, invite := range invites {
		out = append(out, InviteResult{Invite: invite, Success: success, Errors: errors, StatusCode: statusCode, ResponseBody: responseBody})
	}
	return out
}

func (s *WorkspaceService) KickMember(ctx context.Context, accessToken, workspaceID, userID string) ActionResult {
	path := "/backend-api/accounts/" + workspaceID + "/users/" + userID
	headers := s.workspaceHeaders(accessToken, workspaceID, path, "/backend-api/accounts/{account_id}/users/{user_id}", "/admin/members")
	res, payload, err := s.do(ctx, http.MethodDelete, s.BaseURL+path, nil, headers)
	return buildActionResult(res, payload, err)
}

func (s *WorkspaceService) listPaged(ctx context.Context, accessToken, workspaceID, resource, refererPath string) ([]map[string]any, error) {
	out := []map[string]any{}
	for page := 0; ; page++ {
		offset := page * memberPageSize
		path := fmt.Sprintf("/backend-api/accounts/%s/%s", workspaceID, resource)
		rawURL := fmt.Sprintf("%s%s?limit=%d&offset=%d", s.BaseURL, path, memberPageSize, offset)
		headers := s.workspaceHeaders(accessToken, workspaceID, path, "/backend-api/accounts/{account_id}/"+resource, refererPath)
		res, body, err := s.do(ctx, http.MethodGet, rawURL, nil, headers)
		if err != nil {
			return nil, err
		}
		if res.StatusCode >= 400 {
			return nil, fmt.Errorf("list %s failed: HTTP %d: %s", resource, res.StatusCode, summarize(body))
		}
		items, err := decodeItems(body)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		out = append(out, items...)
	}
	return out, nil
}

func (s *WorkspaceService) do(ctx context.Context, method, rawURL string, body io.Reader, headers map[string]string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := s.client().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	return res, payload, err
}

func (s *WorkspaceService) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (s *WorkspaceService) authHeaders(accessToken string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + accessToken}
}

func (s *WorkspaceService) workspaceHeaders(accessToken, workspaceID, targetPath, targetRoute, refererPath string) map[string]string {
	headers := s.authHeaders(accessToken)
	headers["Accept"] = "*/*"
	headers["Content-Type"] = "application/json"
	headers["ChatGPT-Account-ID"] = workspaceID
	headers["Origin"] = s.BaseURL
	headers["Referer"] = s.BaseURL + refererPath
	headers["OAI-Device-ID"] = s.ensureDeviceID()
	headers["OAI-Language"] = firstNonEmpty(s.Language, "en-US")
	headers["OAI-Session-ID"] = s.ensureSessionID()
	headers["X-OpenAI-Target-Path"] = targetPath
	headers["X-OpenAI-Target-Route"] = targetRoute
	return headers
}

func (s *WorkspaceService) ensureDeviceID() string {
	if s.deviceID == "" {
		s.deviceID = randomToken(16)
	}
	return s.deviceID
}

func (s *WorkspaceService) ensureSessionID() string {
	if s.sessionID == "" {
		s.sessionID = randomToken(24)
	}
	return s.sessionID
}

func IsPersonalPlanType(planType string) bool {
	return personalPlanTypes[strings.ToLower(strings.TrimSpace(planType))] || strings.HasPrefix(strings.ToLower(strings.TrimSpace(planType)), "personal/")
}

func IsTeamPlanType(planType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(planType))
	for _, prefix := range teamPlanPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func decodeItems(body []byte) ([]map[string]any, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items := []any{}
	switch value := payload.(type) {
	case []any:
		items = value
	case map[string]any:
		items, _ = value["items"].([]any)
	}
	out := []map[string]any{}
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func buildActionResult(res *http.Response, body []byte, err error) ActionResult {
	success := err == nil && res.StatusCode >= 200 && res.StatusCode < 300
	if success {
		return ActionResult{Success: true}
	}
	status := statusOf(res)
	requestURL := ""
	if res != nil && res.Request != nil {
		requestURL = res.Request.URL.String()
	}
	return ActionResult{Success: false, Errors: extractErrors(status, body, err), StatusCode: status, RequestURL: requestURL, ResponseBody: summarize(body)}
}

func extractErrors(statusCode int, body []byte, err error) []string {
	messages := []string{}
	if statusCode > 0 {
		messages = append(messages, fmt.Sprintf("http_%d", statusCode))
	}
	if err != nil {
		messages = append(messages, err.Error())
	}
	var payload any
	if json.Unmarshal(body, &payload) == nil {
		messages = append(messages, collectErrorMessages(payload)...)
	}
	if len(messages) == 0 && len(body) > 0 {
		messages = append(messages, summarize(body))
	}
	return dedupe(messages)
}

func collectErrorMessages(payload any) []string {
	out := []string{}
	switch value := payload.(type) {
	case map[string]any:
		for _, key := range []string{"message", "detail"} {
			if text := strings.TrimSpace(fmt.Sprint(value[key])); text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		if errorObj, ok := value["error"].(map[string]any); ok {
			for _, key := range []string{"message", "code"} {
				if text := strings.TrimSpace(fmt.Sprint(errorObj[key])); text != "" && text != "<nil>" {
					out = append(out, text)
				}
			}
		} else if text := strings.TrimSpace(fmt.Sprint(value["error"])); text != "" && text != "<nil>" {
			out = append(out, text)
		}
	case []any:
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func inviteErrors(invites []Invite, message string) []InviteResult {
	out := []InviteResult{}
	for _, invite := range invites {
		out = append(out, InviteResult{Invite: invite, Errors: []string{message}})
	}
	return out
}

func statusOf(res *http.Response) int {
	if res == nil {
		return 0
	}
	return res.StatusCode
}

func summarize(body []byte) string {
	compact := strings.Join(strings.Fields(string(body)), " ")
	if len(compact) > 4000 {
		return compact[:4000]
	}
	return compact
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func randomToken(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}
