package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type OAuthReplayCandidate struct {
	Index    int
	Email    string
	Password string
	RunDir   string
}

type OAuthReplayResult struct {
	Candidate OAuthReplayCandidate `json:"candidate"`
	Success   bool                 `json:"success"`
	Error     string               `json:"error,omitempty"`
}

func FindReplayCandidates(runDir string) ([]OAuthReplayCandidate, error) {
	entries, err := os.ReadDir(filepath.Join(runDir, "accounts"))
	if err != nil {
		return nil, err
	}
	out := []OAuthReplayCandidate{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(runDir, "accounts", entry.Name(), "account.json")
		payload, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var account map[string]any
		if json.Unmarshal(payload, &account) != nil {
			continue
		}
		email, _ := account["email"].(string)
		password, _ := account["password"].(string)
		if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
			continue
		}
		idx := 0
		if v, ok := account["index"].(float64); ok {
			idx = int(v)
		}
		out = append(out, OAuthReplayCandidate{Index: idx, Email: email, Password: password, RunDir: runDir})
	}
	return out, nil
}
