package proxy

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Stage string

const (
	StageRegister Stage = "register"
	StageOAuth    Stage = "oauth"
	StageTeam     Stage = "team"
)

type Allocation struct {
	Stage         Stage  `json:"stage"`
	AccountIndex  int    `json:"account_index"`
	Proxy         string `json:"proxy,omitempty"`
	PreviousProxy string `json:"previous_proxy,omitempty"`
}

type NovadaConfig struct {
	Username      string
	Password      string
	Endpoint      string
	Zone          string
	Region        string
	State         string
	ASN           string
	SessionPrefix string
}

type PoolConfig struct {
	Enabled         bool
	Proxies         []string
	StageFlags      map[Stage]bool
	RotateOnFailure bool
	StatePath       string
	Novada          *NovadaConfig
}

type Pool struct {
	enabled         bool
	proxies         []string
	stageFlags      map[Stage]bool
	rotateOnFailure bool
	statePath       string
	novada          *NovadaConfig
	mu              sync.Mutex
	stageOffsets    map[string]int
	assignments     map[string]string
	rotations       map[string]int
	failed          map[string]map[string]bool
}

func NewPool(cfg PoolConfig) *Pool {
	stageFlags := map[Stage]bool{StageRegister: true, StageOAuth: true, StageTeam: true}
	for stage, enabled := range cfg.StageFlags {
		stageFlags[stage] = enabled
	}
	p := &Pool{
		enabled:         cfg.Enabled,
		proxies:         normalizeList(cfg.Proxies),
		stageFlags:      stageFlags,
		rotateOnFailure: cfg.RotateOnFailure,
		statePath:       cfg.StatePath,
		novada:          cfg.Novada,
		stageOffsets:    map[string]int{},
		assignments:     map[string]string{},
		rotations:       map[string]int{},
		failed:          map[string]map[string]bool{},
	}
	p.loadFailed()
	return p
}

func (p *Pool) ProxyFor(stage Stage, accountIndex int) string {
	if !p.enabled || !p.stageFlags[stage] {
		return ""
	}
	scope := scopeForStage(stage)
	key := assignmentKey(scope, accountIndex)
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing := p.assignments[key]; existing != "" {
		return existing
	}
	if p.novada != nil {
		value := p.buildNovadaProxy(accountIndex, 0)
		p.assignments[key] = value
		return value
	}
	if len(p.proxies) == 0 {
		return ""
	}
	offset := p.stageOffsets[scope]
	value := p.selectProxy(scope, offset)
	p.assignments[key] = value
	p.stageOffsets[scope] = offset + 1
	return value
}

func (p *Pool) MarkFailure(stage Stage, accountIndex int, currentProxy string) *Allocation {
	if !p.rotateOnFailure {
		return nil
	}
	scope := scopeForStage(stage)
	key := assignmentKey(scope, accountIndex)
	p.mu.Lock()
	defer p.mu.Unlock()
	current := strings.TrimSpace(currentProxy)
	if current == "" {
		current = p.assignments[key]
	}
	if current == "" {
		return nil
	}
	if p.novada != nil {
		rotation := p.rotations[key] + 1
		p.rotations[key] = rotation
		next := p.buildNovadaProxy(accountIndex, rotation)
		p.assignments[key] = next
		return &Allocation{Stage: stage, AccountIndex: accountIndex, Proxy: next, PreviousProxy: current}
	}
	if len(p.proxies) == 0 {
		return nil
	}
	if p.failed[scope] == nil {
		p.failed[scope] = map[string]bool{}
	}
	p.failed[scope][current] = true
	p.persistFailed()
	if len(p.proxies) == 1 {
		return &Allocation{Stage: stage, AccountIndex: accountIndex, Proxy: current, PreviousProxy: current}
	}
	currentIndex := indexOf(p.proxies, current)
	if currentIndex < 0 {
		currentIndex = p.stageOffsets[scope]
	}
	next := p.selectProxy(scope, currentIndex+1)
	p.assignments[key] = next
	return &Allocation{Stage: stage, AccountIndex: accountIndex, Proxy: next, PreviousProxy: current}
}

func (p *Pool) ProxyCount() int {
	if p.novada != nil {
		return 5
	}
	return len(p.proxies)
}

func (p *Pool) AllProxies() []string {
	out := make([]string, len(p.proxies))
	copy(out, p.proxies)
	return out
}

func (p *Pool) WithProxies(proxies []string) *Pool {
	return NewPool(PoolConfig{Enabled: p.enabled, Proxies: proxies, StageFlags: p.stageFlags, RotateOnFailure: p.rotateOnFailure, StatePath: p.statePath, Novada: p.novada})
}

func (p *Pool) selectProxy(scope string, offset int) string {
	failed := p.failed[scope]
	for i := 0; i < len(p.proxies); i++ {
		candidate := p.proxies[(offset+i)%len(p.proxies)]
		if !failed[candidate] {
			return candidate
		}
	}
	return p.proxies[offset%len(p.proxies)]
}

func (p *Pool) buildNovadaProxy(accountIndex, rotation int) string {
	cfg := p.novada
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = "gate.novada.com:1000"
	}
	parts := []string{strings.TrimSpace(cfg.Username), "zone-" + strings.TrimSpace(cfg.Zone)}
	if strings.TrimSpace(cfg.ASN) != "" {
		parts = append(parts, "asn-"+strings.TrimSpace(cfg.ASN))
	} else if strings.TrimSpace(cfg.Region) != "" {
		parts = append(parts, "region-"+strings.TrimSpace(cfg.Region))
		if strings.TrimSpace(cfg.State) != "" {
			parts = append(parts, "st-"+strings.TrimSpace(cfg.State))
		}
	}
	prefix := strings.TrimSpace(cfg.SessionPrefix)
	if prefix == "" {
		prefix = "acct"
	}
	parts = append(parts, fmt.Sprintf("sessid-%s%04dr%d", prefix, accountIndex, rotation))
	userinfo := url.UserPassword(strings.Join(parts, "-"), cfg.Password).String()
	return "http://" + userinfo + "@" + endpoint
}

func (p *Pool) loadFailed() {
	if p.statePath == "" {
		return
	}
	payload, err := os.ReadFile(p.statePath)
	if err != nil {
		return
	}
	var raw map[string][]string
	if json.Unmarshal(payload, &raw) != nil {
		return
	}
	for scope, proxies := range raw {
		p.failed[scope] = map[string]bool{}
		for _, candidate := range proxies {
			if indexOf(p.proxies, candidate) >= 0 {
				p.failed[scope][candidate] = true
			}
		}
	}
}

func (p *Pool) persistFailed() {
	if p.statePath == "" {
		return
	}
	raw := map[string][]string{}
	for scope, failed := range p.failed {
		for candidate := range failed {
			raw[scope] = append(raw[scope], candidate)
		}
	}
	payload, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p.statePath), 0o755)
	_ = os.WriteFile(p.statePath, append(payload, '\n'), 0o644)
}

func scopeForStage(stage Stage) string {
	switch stage {
	case StageRegister, StageOAuth, StageTeam:
		return "account"
	default:
		return string(stage)
	}
}

func assignmentKey(scope string, accountIndex int) string {
	return fmt.Sprintf("%s:%d", scope, accountIndex)
}

func normalizeList(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func indexOf(values []string, needle string) int {
	for idx, value := range values {
		if value == needle {
			return idx
		}
	}
	return -1
}
