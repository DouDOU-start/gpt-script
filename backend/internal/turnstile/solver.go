package turnstile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chatgpt-register-script/internal/automation"
)

type Browser interface {
	Navigate(string) error
	Eval(string, any) error
	Type(string, string) error
	Click(string) error
	WaitVisible(string, time.Duration) error
	Close(context.Context)
}

type Options struct {
	Email       string
	Password    string
	PasswordURL string
	DeviceID    string
	Timeout     time.Duration
	Progress    func(string)
}

type Result struct {
	Token string `json:"token"`
	URL   string `json:"url,omitempty"`
}

func SolveWithBrowser(ctx context.Context, browser Browser, opts Options) (Result, error) {
	if browser == nil {
		return Result{}, fmt.Errorf("browser is required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	progress := func(message string) {
		if opts.Progress != nil {
			opts.Progress(message)
		}
	}
	progress("installing sentinel token capture hooks")
	if err := browser.Eval(captureScript(), nil); err != nil {
		return Result{}, err
	}
	url := strings.TrimSpace(opts.PasswordURL)
	if url == "" {
		url = authorizeURL(opts.Email, opts.DeviceID)
	}
	progress("navigating browser for sentinel token capture")
	if err := browser.Navigate(url); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(opts.Email) != "" && opts.PasswordURL == "" {
		_ = browser.WaitVisible(`input[type="email"], input[name="email"]`, 10*time.Second)
		_ = browser.Type(`input[type="email"], input[name="email"]`, opts.Email)
		_ = browser.Click(`button[type="submit"], button[name="action"], button[data-action-button-primary="true"]`)
	}
	if strings.TrimSpace(opts.Password) != "" {
		_ = browser.WaitVisible(`input[type="password"], input[name="password"]`, 20*time.Second)
		_ = browser.Type(`input[type="password"], input[name="password"]`, opts.Password)
		_ = browser.Click(`button[type="submit"], button[name="action"], button[data-action-button-primary="true"]`)
	}
	deadline := time.NewTimer(opts.Timeout)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-deadline.C:
			return Result{}, fmt.Errorf("sentinel token capture timed out")
		case <-tick.C:
			var token string
			if err := browser.Eval(`() => window.__openAISentinelToken || ""`, &token); err != nil {
				continue
			}
			if len(strings.TrimSpace(token)) > 50 {
				return Result{Token: strings.TrimSpace(token), URL: url}, nil
			}
		}
	}
}

func SolveWithManager(ctx context.Context, manager *automation.Manager, opts Options) (Result, error) {
	return SolveWithBrowser(ctx, manager, opts)
}

func captureScript() string {
	return `() => {
		if (window.__openAISentinelCaptureInstalled) return true;
		window.__openAISentinelCaptureInstalled = true;
		window.__openAISentinelToken = window.__openAISentinelToken || "";
		const captureHeaders = (headers) => {
			try {
				if (!headers) return;
				if (headers instanceof Headers) {
					const token = headers.get("openai-sentinel-token") || headers.get("OpenAI-Sentinel-Token");
					if (token) window.__openAISentinelToken = token;
					return;
				}
				if (Array.isArray(headers)) {
					for (const [key, value] of headers) {
						if (String(key).toLowerCase() === "openai-sentinel-token" && value) window.__openAISentinelToken = String(value);
					}
					return;
				}
				for (const key of Object.keys(headers)) {
					if (key.toLowerCase() === "openai-sentinel-token" && headers[key]) window.__openAISentinelToken = String(headers[key]);
				}
			} catch (_) {}
		};
		const originalFetch = window.fetch;
		window.fetch = function(input, init) {
			captureHeaders(init && init.headers);
			if (input && input.headers) captureHeaders(input.headers);
			return originalFetch.apply(this, arguments);
		};
		const originalSetRequestHeader = XMLHttpRequest.prototype.setRequestHeader;
		XMLHttpRequest.prototype.setRequestHeader = function(name, value) {
			if (String(name).toLowerCase() === "openai-sentinel-token" && value) window.__openAISentinelToken = String(value);
			return originalSetRequestHeader.apply(this, arguments);
		};
		return true;
	}`
}

func authorizeURL(email, deviceID string) string {
	base := "https://auth.openai.com/api/accounts/authorize?client_id=app_X8zY6vW2pQ9tR3dE7nK1jL5gH&scope=openid%20email%20profile%20offline_access&response_type=code&redirect_uri=https%3A%2F%2Fchatgpt.com%2Fapi%2Fauth%2Fcallback%2Fopenai&audience=https%3A%2F%2Fapi.openai.com%2Fv1&prompt=login&screen_hint=login_or_signup"
	if strings.TrimSpace(email) != "" {
		base += "&login_hint=" + strings.ReplaceAll(strings.TrimSpace(email), "@", "%40")
	}
	if strings.TrimSpace(deviceID) != "" {
		base += "&device_id=" + strings.TrimSpace(deviceID) + "&ext-oai-did=" + strings.TrimSpace(deviceID)
	}
	return base
}
