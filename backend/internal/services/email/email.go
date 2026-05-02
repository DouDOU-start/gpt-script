package email

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var DefaultCodePattern = regexp.MustCompile(`\b(\d{4,8})\b`)

var DefaultVerificationSubjectKeywords = []string{"code", "verification", "verify", "验证码", "代码"}
var DefaultExcludedSubjectKeywords = []string{"login"}

type Message struct {
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Sender    string `json:"sender,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	SourceID  string `json:"source_id,omitempty"`
}

type CodeMatch struct {
	Value    string `json:"value"`
	Snippet  string `json:"snippet"`
	SourceID string `json:"source_id,omitempty"`
}

type CodeExtractor struct {
	Pattern *regexp.Regexp
}

func (e CodeExtractor) Extract(message Message) []CodeMatch {
	pattern := e.Pattern
	if pattern == nil {
		pattern = DefaultCodePattern
	}
	matches := pattern.FindAllStringSubmatchIndex(message.Body, -1)
	out := []CodeMatch{}
	for _, match := range matches {
		start, end := match[0], match[1]
		if len(match) >= 4 && match[2] >= 0 {
			start, end = match[2], match[3]
		}
		out = append(out, CodeMatch{Value: message.Body[start:end], Snippet: snippet(message.Body, start, end), SourceID: message.SourceID})
	}
	return out
}

func ExtractEmailCodes(message Message, pattern *regexp.Regexp) []CodeMatch {
	return CodeExtractor{Pattern: pattern}.Extract(message)
}

func FirstMatchingCode(messages []Message, opts MatchOptions) *CodeMatch {
	extractor := CodeExtractor{Pattern: opts.Pattern}
	excluded := map[string]bool{}
	for _, sourceID := range opts.ExcludedSourceIDs {
		excluded[sourceID] = true
	}
	for _, message := range messages {
		if excluded[message.SourceID] || !MessageMatches(message, opts.ExpectedRecipient, opts.SubjectKeywords, opts.ExcludedSubjectKeywords, opts.SenderContains) {
			continue
		}
		matches := extractor.Extract(message)
		if len(matches) > 0 {
			return &matches[0]
		}
	}
	return nil
}

type MatchOptions struct {
	Pattern                 *regexp.Regexp
	SubjectKeywords         []string
	ExcludedSubjectKeywords []string
	ExpectedRecipient       string
	SenderContains          string
	ExcludedSourceIDs       []string
}

func ResolveSubjectKeywords(configured []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range append(DefaultVerificationSubjectKeywords, configured...) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func KeywordMatches(subject, keyword string) bool {
	normalized := strings.ToLower(strings.TrimSpace(keyword))
	if normalized == "" {
		return false
	}
	subject = strings.ToLower(subject)
	if regexp.MustCompile(`^[a-z0-9_ -]+$`).MatchString(normalized) {
		return regexp.MustCompile(`(^|[^a-z0-9])`+regexp.QuoteMeta(normalized)+`([^a-z0-9]|$)`).FindStringIndex(subject) != nil
	}
	return strings.Contains(subject, normalized)
}

func MessageMatches(message Message, recipient string, include, exclude []string, senderContains string) bool {
	if recipient != "" && !strings.Contains(strings.ToLower(message.Recipient), strings.ToLower(recipient)) {
		return false
	}
	if senderContains != "" && !strings.Contains(strings.ToLower(message.Sender), strings.ToLower(senderContains)) {
		return false
	}
	subject := strings.ToLower(message.Subject)
	for _, keyword := range exclude {
		if KeywordMatches(subject, keyword) {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, keyword := range include {
		if KeywordMatches(subject, keyword) {
			return true
		}
	}
	return false
}

func snippet(text string, start, end int) string {
	left := start - 16
	if left < 0 {
		left = 0
	}
	right := end + 16
	if right > len(text) {
		right = len(text)
	}
	return strings.TrimSpace(text[left:right])
}

type StoredCode struct {
	Recipient  string    `json:"recipient"`
	Sender     string    `json:"sender,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Body       string    `json:"body,omitempty"`
	Code       string    `json:"code"`
	MessageKey string    `json:"message_key,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
	UsedAt     time.Time `json:"used_at,omitempty"`
}

type VerificationCodeStore struct {
	Path string
	mu   sync.Mutex
}

func NewVerificationCodeStore(path string) *VerificationCodeStore {
	return &VerificationCodeStore{Path: path}
}

func (s *VerificationCodeStore) Save(code StoredCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if code.ReceivedAt.IsZero() {
		code.ReceivedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(code)
}

func (s *VerificationCodeStore) LatestUsedCode(recipient string, maxAge time.Duration, senderContains string, include, exclude []string) string {
	payload, err := os.ReadFile(s.Path)
	if err != nil {
		return ""
	}
	cutoff := time.Now().Add(-maxAge)
	latest := StoredCode{}
	for _, line := range strings.Split(string(payload), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var code StoredCode
		if json.Unmarshal([]byte(line), &code) != nil || code.Code == "" || code.UsedAt.IsZero() || code.UsedAt.Before(cutoff) {
			continue
		}
		message := Message{Subject: code.Subject, Sender: code.Sender, Recipient: code.Recipient}
		if !MessageMatches(message, recipient, include, exclude, senderContains) {
			continue
		}
		if latest.UsedAt.IsZero() || code.UsedAt.After(latest.UsedAt) {
			latest = code
		}
	}
	return latest.Code
}
