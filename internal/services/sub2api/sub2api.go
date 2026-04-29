package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const OpenAIOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

var personalPlanTypes = map[string]bool{
	"free": true, "plus": true, "pro": true,
	"personal/free": true, "personal/plus": true, "personal/pro": true,
}

type TokenBinding struct {
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	PlanType         string `json:"plan_type,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
}

func (b TokenBinding) IsPersonal(teamWorkspaceID string) bool {
	if strings.TrimSpace(b.ChatGPTAccountID) == "" || strings.TrimSpace(b.PlanType) == "" {
		return false
	}
	if teamWorkspaceID != "" && b.ChatGPTAccountID == teamWorkspaceID {
		return false
	}
	return personalPlanTypes[strings.ToLower(strings.TrimSpace(b.PlanType))]
}

func ValidatePersonalTokenBinding(binding TokenBinding, teamWorkspaceID string) error {
	if !binding.IsPersonal(teamWorkspaceID) {
		return fmt.Errorf("oauth token is not bound to a personal workspace")
	}
	return nil
}

type UploadPayload struct {
	WorkspaceID     string            `json:"workspace_id"`
	AccessToken     string            `json:"access_token"`
	RefreshToken    string            `json:"refresh_token,omitempty"`
	IDToken         string            `json:"id_token,omitempty"`
	Email           string            `json:"email,omitempty"`
	ChatGPTUserID   string            `json:"chatgpt_user_id,omitempty"`
	ClientID        string            `json:"client_id"`
	ExpiresAt       string            `json:"expires_at,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	TokenBinding    TokenBinding      `json:"token_binding"`
	TeamWorkspaceID string            `json:"team_workspace_id,omitempty"`
	Concurrency     int               `json:"concurrency"`
	Priority        int               `json:"priority"`
	GroupIDs        []int             `json:"group_ids"`
}

func (p UploadPayload) Validate() error {
	if strings.TrimSpace(p.WorkspaceID) == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if strings.TrimSpace(p.AccessToken) == "" {
		return fmt.Errorf("access_token is required")
	}
	if p.WorkspaceID != p.TokenBinding.ChatGPTAccountID {
		return fmt.Errorf("workspace_id must match token binding")
	}
	return ValidatePersonalTokenBinding(p.TokenBinding, p.TeamWorkspaceID)
}

type UploadResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message,omitempty"`
	RecordID string   `json:"record_id,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

type Service struct {
	Client             *http.Client
	BaseURL            string
	AdminAPIKey        string
	DefaultConcurrency int
	DefaultPriority    int
	DefaultGroupIDs    []int
}

func NewService(baseURL, adminAPIKey string) *Service {
	return &Service{Client: &http.Client{Timeout: 30 * time.Second}, BaseURL: strings.TrimRight(baseURL, "/"), AdminAPIKey: adminAPIKey, DefaultConcurrency: 3, DefaultPriority: 1, DefaultGroupIDs: []int{14}}
}

func (s *Service) PreparePayload(workspaceID, accessToken string, binding TokenBinding) UploadPayload {
	return UploadPayload{WorkspaceID: workspaceID, AccessToken: accessToken, ClientID: OpenAIOAuthClientID, TokenBinding: binding, Concurrency: s.defaultConcurrency(), Priority: s.defaultPriority(), GroupIDs: s.defaultGroupIDs()}
}

func (s *Service) Upload(ctx context.Context, payload UploadPayload) (UploadResult, error) {
	if err := payload.Validate(); err != nil {
		return UploadResult{}, err
	}
	body := map[string]any{
		"name":        firstNonEmpty(payload.Email, payload.Metadata["email"], payload.WorkspaceID),
		"platform":    "openai",
		"type":        "oauth",
		"concurrency": payload.Concurrency,
		"priority":    payload.Priority,
		"group_ids":   payload.GroupIDs,
		"credentials": compact(map[string]string{
			"access_token":       payload.AccessToken,
			"refresh_token":      payload.RefreshToken,
			"id_token":           payload.IDToken,
			"email":              payload.Email,
			"chatgpt_account_id": payload.TokenBinding.ChatGPTAccountID,
			"chatgpt_user_id":    payload.ChatGPTUserID,
			"organization_id":    payload.TokenBinding.OrganizationID,
			"plan_type":          payload.TokenBinding.PlanType,
			"client_id":          payload.ClientID,
			"expires_at":         payload.ExpiresAt,
		}),
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return UploadResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/api/v1/admin/accounts", bytes.NewReader(encoded))
	if err != nil {
		return UploadResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.AdminAPIKey)
	res, err := s.client().Do(req)
	if err != nil {
		return UploadResult{}, err
	}
	defer res.Body.Close()
	var response map[string]any
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return UploadResult{}, err
	}
	result := InterpretResponse(response)
	if res.StatusCode >= 400 {
		result.Success = false
		if len(result.Errors) == 0 {
			result.Errors = []string{fmt.Sprintf("http_%d", res.StatusCode)}
		}
	}
	return result, nil
}

func InterpretResponse(response map[string]any) UploadResult {
	nested, _ := response["data"].(map[string]any)
	recordID := firstNonEmpty(anyString(response["id"]), anyString(nested["id"]))
	message := anyString(response["message"])
	success := response["status"] == "ok" || anyString(response["code"]) == "0" || recordID != ""
	result := UploadResult{Success: success, RecordID: recordID, Message: message}
	if !success {
		result.Errors = []string{"upload_failed"}
	}
	return result
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *Service) defaultConcurrency() int {
	if s.DefaultConcurrency > 0 {
		return s.DefaultConcurrency
	}
	return 3
}

func (s *Service) defaultPriority() int {
	if s.DefaultPriority > 0 {
		return s.DefaultPriority
	}
	return 1
}

func (s *Service) defaultGroupIDs() []int {
	if len(s.DefaultGroupIDs) > 0 {
		return append([]int(nil), s.DefaultGroupIDs...)
	}
	return []int{14}
}

func compact(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
