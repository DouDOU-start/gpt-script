package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type ProgressHeartbeat struct {
	Interval time.Duration
	Logger   func(string)
	mu       sync.Mutex
	stage    string
	detail   string
}

func (p *ProgressHeartbeat) Update(stage, detail string) {
	p.mu.Lock()
	p.stage = stage
	p.detail = detail
	p.mu.Unlock()
}

func (p *ProgressHeartbeat) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	logger := p.Logger
	if logger == nil {
		logger = func(message string) { log.Print(message) }
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			stage, detail := p.stage, p.detail
			p.mu.Unlock()
			if stage != "" || detail != "" {
				logger(fmt.Sprintf("progress stage=%s detail=%s", stage, detail))
			}
		}
	}
}

func BrowserURLContext(accountIndex int, email, url string) map[string]any {
	return map[string]any{"account_index": accountIndex, "email": email, "browser_url": url}
}
