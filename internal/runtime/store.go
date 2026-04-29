package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type PlannedAccount struct {
	Index    int    `json:"index"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type RunContext struct {
	RunID    string           `json:"run_id"`
	RunDir   string           `json:"run_dir"`
	Accounts []PlannedAccount `json:"accounts"`
}

type RunStore struct {
	RunID  string
	RunDir string
	mu     sync.Mutex
}

type Summary struct {
	Command    string `json:"command"`
	Requested  int    `json:"requested"`
	Planned    int    `json:"planned"`
	Registered int    `json:"registered"`
	Invited    int    `json:"invited"`
	Authorized int    `json:"authorized"`
	Uploaded   int    `json:"uploaded"`
	CleanedUp  int    `json:"cleaned_up"`
	Failed     int    `json:"failed"`
}

func CreatePlannedRun(commandName string, accounts []PlannedAccount, runsDir string, metadata map[string]any) (*RunContext, error) {
	if runsDir == "" {
		runsDir = "runs"
	}
	runID := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02T15-04-05.000000+08-00")
	runDir := filepath.Join(runsDir, runID)
	if err := os.MkdirAll(filepath.Join(runDir, "accounts"), 0o755); err != nil {
		return nil, err
	}
	manifest := map[string]any{
		"run_id":     runID,
		"command":    commandName,
		"count":      len(accounts),
		"created_at": timestampNow(),
	}
	for key, value := range metadata {
		manifest[key] = value
	}
	if err := writeJSON(filepath.Join(runDir, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(runDir, "summary.json"), Summary{Command: commandName, Requested: len(accounts), Planned: len(accounts)}); err != nil {
		return nil, err
	}
	store := RunStore{RunID: runID, RunDir: runDir}
	store.AppendRunEvent("plan", "created", map[string]any{"command": commandName, "requested": len(accounts)})
	for _, account := range accounts {
		if err := store.WriteAccount(account.Index, map[string]any{
			"index":        account.Index,
			"email":        account.Email,
			"password":     account.Password,
			"profile_name": account.Name,
			"status":       "planned",
			"profile": map[string]any{
				"name":     account.Name,
				"email":    account.Email,
				"password": account.Password,
			},
			"register": map[string]any{"status": "planned"},
			"team":     map[string]any{"status": "pending"},
			"oauth":    map[string]any{"status": "pending"},
			"sub2api":  map[string]any{"status": "pending"},
		}); err != nil {
			return nil, err
		}
		store.AppendAccountEvent(account.Index, "plan", "created", map[string]any{"email": account.Email})
	}
	return &RunContext{RunID: runID, RunDir: runDir, Accounts: accounts}, nil
}

func StoreFromContext(ctx RunContext) RunStore {
	return RunStore{RunID: ctx.RunID, RunDir: ctx.RunDir}
}

func (s *RunStore) AccountDir(index int) string {
	dir := filepath.Join(s.RunDir, "accounts", fmt.Sprintf("%04d", index))
	_ = os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755)
	return dir
}

func (s *RunStore) LoadAccount(index int) (map[string]any, error) {
	path := filepath.Join(s.AccountDir(index), "account.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var account map[string]any
	if err := json.Unmarshal(payload, &account); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *RunStore) WriteAccount(index int, account map[string]any) error {
	return writeJSON(filepath.Join(s.AccountDir(index), "account.json"), account)
}

func (s *RunStore) UpdateAccount(index int, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, err := s.LoadAccount(index)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if account == nil {
		account = map[string]any{"index": index}
	}
	deepMerge(account, updates)
	return s.WriteAccount(index, account)
}

func (s *RunStore) WriteAccountArtifact(index int, name string, value any) (string, error) {
	path := filepath.Join(s.AccountDir(index), "artifacts", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if bytes, ok := value.([]byte); ok {
		return path, os.WriteFile(path, bytes, 0o644)
	}
	return path, writeJSON(path, value)
}

func (s *RunStore) AppendRunEvent(stage, status string, metadata map[string]any) map[string]any {
	event := buildEvent(stage, status, metadata)
	s.appendJSONL(filepath.Join(s.RunDir, "events.jsonl"), event)
	s.appendTimeline(filepath.Join(s.RunDir, "timeline.log"), event)
	return event
}

func (s *RunStore) AppendAccountEvent(index int, stage, status string, metadata map[string]any) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["account_index"] = index
	event := buildEvent(stage, status, metadata)
	s.appendJSONL(filepath.Join(s.AccountDir(index), "events.jsonl"), event)
	s.appendTimeline(filepath.Join(s.AccountDir(index), "timeline.log"), event)
	return event
}

func (s *RunStore) AppendStageEvent(index int, email, stage, status, detail string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["email"] = email
	if detail != "" {
		metadata["detail"] = detail
	}
	s.AppendAccountEvent(index, stage, status, metadata)
	s.AppendRunEvent(stage, status, metadata)
}

func (s *RunStore) AppendHTTPCapture(record map[string]any) {
	s.appendJSONL(filepath.Join(s.RunDir, "http_capture.jsonl"), buildEvent("http", "captured", record))
}

func (s *RunStore) IncrementSummary(field string, delta int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.RunDir, "summary.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var summary map[string]any
	if err := json.Unmarshal(payload, &summary); err != nil {
		return err
	}
	current, _ := summary[field].(float64)
	summary[field] = int(current) + delta
	return writeJSON(path, summary)
}

func buildEvent(stage, status string, metadata map[string]any) map[string]any {
	event := map[string]any{"timestamp": timestampNow(), "stage": stage, "status": status}
	for key, value := range metadata {
		event[key] = value
	}
	return event
}

func (s *RunStore) appendJSONL(path string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = appendJSONL(path, value)
}

func (s *RunStore) appendTimeline(path string, event map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = appendTimeline(path, event)
}

func appendJSONL(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(value)
}

func appendTimeline(path string, event map[string]any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return appendLine(path, string(payload))
}

func appendLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o644)
}

func timestampNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func deepMerge(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if srcMap, ok := value.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[key] = value
	}
}
