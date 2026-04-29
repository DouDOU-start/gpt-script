package automation

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type SessionLite struct {
	AccessToken string          `json:"accessToken"`
	Expires     string          `json:"expires"`
	Account     *SessionAccount `json:"account"`
	Error       *string         `json:"error"`
	Raw         map[string]any  `json:"-"`
	RawAccount  map[string]any  `json:"-"`
}

type SessionAccount struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	PlanType       string `json:"planType"`
}

func (b *Browser) FetchSessionLite() (SessionLite, error) {
	var out SessionLite
	if b.page == nil {
		if _, err := b.NewPage(); err != nil {
			return out, err
		}
	}

	// Only extract minimal fields to reduce schema sensitivity.
	script := `async () => {
		try {
			const response = await fetch('https://chatgpt.com/api/auth/session');
			if (!response.ok) return { error: 'http ' + response.status };
			const data = await response.json();
			return { accessToken: data.accessToken, expires: data.expires, account: data.account };
		} catch (e) {
			return { error: String(e && e.message ? e.message : e) };
		}
	}`
	if err := b.Eval(script, &out); err != nil {
		return SessionLite{}, fmt.Errorf("eval session script: %w", err)
	}
	return out, nil
}

// ParseUserIDFromJWT tries to extract the user_id from ChatGPT's JWT access token.
func ParseUserIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}

	payload := parts[1]
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}

	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if userID, ok := auth["user_id"].(string); ok {
			return userID
		}
	}

	if sub, ok := claims["sub"].(string); ok {
		return sub
	}

	return ""
}
