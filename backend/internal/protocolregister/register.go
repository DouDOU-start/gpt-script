package protocolregister

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"chatgpt-register-script/internal/oauth"
)

const (
	baseURL          = "https://chatgpt.com"
	authBaseURL      = "https://auth.openai.com"
	defaultLocale    = "en-US,en;q=0.9"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.7001.90 Safari/537.36"
)

type Account struct {
	Email     string
	Password  string
	Name      string
	Birthdate string
}

type Options struct {
	ProxyURL         string
	UserAgent        string
	Locale           string
	Timeout          time.Duration
	CodeTimeout      time.Duration
	Progress         func(string)
	VerificationCode func(email string) (string, error)
}

type Result struct {
	AccessToken string
	Expires     string
	WorkspaceID string
	OrgID       string
	PlanType    string
	IsTeam      bool
	Cookies     []oauth.Cookie
	RawSession  map[string]any
}

type routeState string

const (
	stateLanding  routeState = "landing"
	statePassword routeState = "password"
	stateEmailOTP routeState = "email_otp"
	stateAboutYou routeState = "about_you"
	stateSession  routeState = "session_ready"
	stateError    routeState = "error"
	stateUnknown  routeState = "unknown"
)

type route struct {
	PageType    string
	ContinueURL string
	FinalURL    string
}

type Driver struct {
	session              *oauth.Session
	userAgent            string
	locale               string
	csrfToken            string
	authSessionLoggingID string
	deviceID             string
	continueURLCache     map[string]string
	artifactCache        map[string]map[string]string
}

func Run(ctx context.Context, account Account, opts Options) (*Result, error) {
	account.Email = strings.TrimSpace(account.Email)
	if account.Email == "" || strings.TrimSpace(account.Password) == "" {
		return nil, fmt.Errorf("邮箱和密码不能为空")
	}
	if strings.TrimSpace(account.Name) == "" {
		account.Name = "ChatGPT User"
	}
	if strings.TrimSpace(account.Birthdate) == "" {
		account.Birthdate = birthdateForEmail(account.Email)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.CodeTimeout <= 0 {
		opts.CodeTimeout = 2 * time.Minute
	}
	userAgent := firstNonEmpty(opts.UserAgent, defaultUserAgent)
	locale := firstNonEmpty(opts.Locale, defaultLocale)
	sess, err := oauth.NewSession(opts.ProxyURL, userAgent, locale, opts.Timeout)
	if err != nil {
		return nil, err
	}
	d := &Driver{
		session:          sess,
		userAgent:        userAgent,
		locale:           locale,
		continueURLCache: map[string]string{},
		artifactCache:    map[string]map[string]string{},
	}
	progress := func(detail string) {
		if opts.Progress != nil {
			opts.Progress(detail)
		}
	}

	progress("bootstrapping register flow")
	rt, err := d.bootstrap(account)
	if err != nil {
		return nil, err
	}
	for steps := 0; steps < 20; steps++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("任务已取消: %w", ctx.Err())
		default:
		}
		s := classify(rt)
		switch s {
		case stateLanding:
			progress("submitting register email")
			rt, err = d.submitEmail(account)
		case statePassword:
			progress("submitting register password")
			rt, err = d.submitPassword(account)
		case stateEmailOTP:
			if opts.VerificationCode == nil {
				return nil, fmt.Errorf("需要邮箱验证码，但未设置验证码获取回调")
			}
			progress("waiting for register email verification code")
			code, codeErr := waitForCode(ctx, account.Email, opts.CodeTimeout, opts.VerificationCode)
			if codeErr != nil {
				return nil, codeErr
			}
			progress("submitting register email verification code")
			rt, err = d.submitEmailCode(account, code)
		case stateAboutYou:
			progress("submitting register profile")
			rt, err = d.submitProfile(account)
		case stateSession:
			progress("bridging chatgpt session")
			if err = d.bridgeSignupSession(account); err != nil {
				return nil, err
			}
			return d.fetchSession(account)
		case stateError:
			return nil, fmt.Errorf("register flow hit error_page")
		default:
			if looksSessionReady(rt.FinalURL) || looksSessionReady(rt.ContinueURL) {
				if err = d.bridgeSignupSession(account); err != nil {
					return nil, err
				}
				return d.fetchSession(account)
			}
			return nil, fmt.Errorf("register flow reached unsupported state: %s", s)
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("register flow exceeded maximum steps")
}

func (d *Driver) bootstrap(account Account) (route, error) {
	d.authSessionLoggingID = randomUUIDish()
	if err := d.warmSession(); err != nil {
		return route{}, err
	}
	humanDelay(500, 1200)
	if err := d.refreshCSRF(); err != nil {
		return route{}, err
	}
	d.ensureDeviceID()
	return route{PageType: "authorize_landing", FinalURL: authBaseURL + "/authorize"}, nil
}

func (d *Driver) submitEmail(account Account) (route, error) {
	humanDelay(800, 2000)
	authorizeURL, err := d.signinURL(account.Email)
	if err != nil {
		return route{}, err
	}
	humanDelay(300, 800)
	authorizeRes, err := d.session.Get(authorizeURL, nil, header(map[string]string{"Referer": baseURL + "/"}), true)
	if err != nil {
		return route{}, err
	}
	if err := authorizeRes.RaiseForStatus(); err != nil {
		return route{}, err
	}
	d.maybeSeedDeviceID(authorizeRes)
	if classify(route{FinalURL: authorizeRes.URL}) == stateError {
		return route{FinalURL: authorizeRes.URL}, nil
	}
	res, err := d.postAuthorizeContinue(account.Email, d.ensureDeviceID(), authorizeRes.URL, "signup")
	if err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res, authorizeRes.URL, "submit_email")
}

func (d *Driver) submitPassword(account Account) (route, error) {
	humanDelay(1000, 2500)
	res, err := d.postWithSentinelRecovery(authBaseURL+"/api/accounts/user/register", map[string]any{
		"password": account.Password,
		"username": account.Email,
	}, authBaseURL+"/create-account/password", "username_password_create", account.Email)
	if err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res, authBaseURL+"/create-account/password", "submit_password")
}

func (d *Driver) submitEmailCode(account Account, code string) (route, error) {
	humanDelay(600, 1500)
	res, err := d.session.PostJSON(authBaseURL+"/api/accounts/email-otp/validate", map[string]any{"code": strings.TrimSpace(code)}, header(map[string]string{
		"Accept":  "application/json",
		"Origin":  authBaseURL,
		"Referer": authBaseURL + "/email-verification",
	}))
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res, authBaseURL+"/email-verification", "submit_email_code")
}

func (d *Driver) submitProfile(account Account) (route, error) {
	humanDelay(800, 2000)
	res, err := d.postWithSentinelRecovery(authBaseURL+"/api/accounts/create_account", map[string]any{
		"name":      account.Name,
		"birthdate": account.Birthdate,
	}, authBaseURL+"/about-you", "oauth_create_account", "")
	if err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res, authBaseURL+"/about-you", "submit_profile")
}

func (d *Driver) bridgeSignupSession(account Account) error {
	if err := d.refreshCSRF(); err != nil {
		return err
	}
	authorizeURL, err := d.signinURL(account.Email)
	if err != nil {
		return err
	}
	res, err := d.session.Get(authorizeURL, nil, header(map[string]string{"Referer": baseURL + "/"}), true)
	if err != nil {
		return err
	}
	if err := res.RaiseForStatus(); err != nil {
		return err
	}
	d.maybeSeedDeviceID(res)
	return nil
}

func (d *Driver) fetchSession(account Account) (*Result, error) {
	if err := d.warmSession(); err != nil {
		return nil, err
	}
	res, err := d.session.Get(baseURL+"/api/auth/session", nil, header(map[string]string{
		"Accept":  "application/json",
		"Referer": baseURL + "/",
	}), true)
	if err != nil {
		return nil, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := res.JSON(&payload); err != nil {
		return nil, err
	}
	accessToken := stringFromAny(payload["accessToken"])
	if accessToken == "" {
		return nil, fmt.Errorf("session 中未找到 accessToken")
	}
	accountPayload, _ := payload["account"].(map[string]any)
	workspaceID := ""
	orgID := ""
	planType := ""
	isTeam := false
	if accountPayload != nil {
		workspaceID = stringFromAny(accountPayload["id"])
		orgID = stringFromAny(accountPayload["organizationId"])
		planType = stringFromAny(accountPayload["planType"])
		isTeam = planType == "team" || planType == "enterprise"
	}
	if workspaceID == "" {
		workspaceID = jwtUserID(accessToken)
	}
	if workspaceID == "" {
		workspaceID = d.cachedArtifact("submit_profile", "workspace_id")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("session 中未找到 workspaceID")
	}
	if planType == "" {
		planType = "free"
	}
	return &Result{
		AccessToken: accessToken,
		Expires:     stringFromAny(payload["expires"]),
		WorkspaceID: workspaceID,
		OrgID:       orgID,
		PlanType:    planType,
		IsTeam:      isTeam,
		Cookies:     d.session.CookieSnapshot(baseURL, authBaseURL),
		RawSession:  payload,
	}, nil
}

func (d *Driver) refreshCSRF() error {
	res, err := d.session.Get(baseURL+"/api/auth/csrf", nil, header(map[string]string{
		"Accept":  "application/json",
		"Origin":  baseURL,
		"Referer": baseURL + "/",
	}), true)
	if err != nil {
		return err
	}
	if err := res.RaiseForStatus(); err != nil {
		return err
	}
	var payload map[string]any
	if err := res.JSON(&payload); err != nil {
		return err
	}
	d.csrfToken = stringFromAny(payload["csrfToken"])
	if d.csrfToken == "" {
		return fmt.Errorf("chatgpt csrf token missing")
	}
	return nil
}

func (d *Driver) signinURL(email string) (string, error) {
	form := url.Values{}
	form.Set("callbackUrl", baseURL+"/")
	form.Set("csrfToken", d.csrfToken)
	form.Set("json", "true")
	res, err := d.session.PostForm(baseURL+"/api/auth/signin/openai?"+d.signinParams(email).Encode(), form, header(map[string]string{
		"Accept":  "application/json",
		"Origin":  baseURL,
		"Referer": baseURL + "/",
	}))
	if err != nil {
		return "", err
	}
	if err := res.RaiseForStatus(); err != nil {
		return "", err
	}
	var payload map[string]any
	if err := res.JSON(&payload); err != nil {
		return "", err
	}
	authorizeURL := stringFromAny(payload["url"])
	if authorizeURL == "" {
		return "", fmt.Errorf("chatgpt signin/openai missing authorize url")
	}
	return authorizeURL, nil
}

func (d *Driver) signinParams(email string) url.Values {
	params := url.Values{}
	params.Set("prompt", "login")
	params.Set("screen_hint", "login_or_signup")
	params.Set("login_hint", email)
	params.Set("ext-oai-did", d.ensureDeviceID())
	params.Set("auth_session_logging_id", d.ensureAuthSessionLoggingID())
	params.Set("ext-passkey-client-capabilities", "1111")
	return params
}

func (d *Driver) postAuthorizeContinue(email, deviceID, referer, screenHint string) (*oauth.HTTPResult, error) {
	payload := map[string]any{"username": map[string]any{"kind": "email", "value": email}}
	if screenHint != "" {
		payload["screen_hint"] = screenHint
	}
	var res *oauth.HTTPResult
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		res, err = d.session.PostJSON(authBaseURL+"/api/accounts/authorize/continue", payload, d.securityHeaders(deviceID, referer, "authorize_continue", true, email))
		if err != nil {
			return nil, err
		}
		if res.StatusCode != 429 || attempt == 2 {
			break
		}
		time.Sleep(parseRetryAfter(res, 10+float64(attempt)*5))
	}
	if err := res.RaiseForStatus(); err != nil {
		return nil, err
	}
	return res, nil
}

func (d *Driver) postWithSentinelRecovery(rawURL string, payload map[string]any, referer, flow, email string) (*oauth.HTTPResult, error) {
	deviceID := d.ensureDeviceID()
	includeSentinel := true
	conflicts := 0
	var res *oauth.HTTPResult
	var err error
	for i := 0; i < 4; i++ {
		res, err = d.session.PostJSON(rawURL, payload, d.securityHeaders(deviceID, referer, flow, includeSentinel, email))
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusConflict {
			if err := res.RaiseForStatus(); err != nil {
				return nil, err
			}
			return res, nil
		}
		conflicts++
		if conflicts >= 2 {
			includeSentinel = false
		}
	}
	if res != nil {
		return res, res.RaiseForStatus()
	}
	return nil, err
}

func (d *Driver) routeFromPayloadResponse(res *oauth.HTTPResult, referer, scope string) (route, error) {
	payload := safeJSON(res)
	continueURL := d.resolveContinueURL(res, payload, scope)
	d.rememberContinueURL(scope, continueURL)
	d.rememberArtifacts(scope, payload, continueURL)
	payload = d.mergeArtifacts(scope, payload)
	finalURL := res.URL
	if continueURL != "" {
		follow, err := d.session.Get(continueURL, nil, header(map[string]string{"Referer": referer}), true)
		if err != nil {
			return route{}, err
		}
		if err := follow.RaiseForStatus(); err != nil {
			return route{}, err
		}
		d.maybeSeedDeviceID(follow)
		finalURL = follow.URL
	}
	return d.routeFromPayload(payload, finalURL, continueURL), nil
}

func (d *Driver) routeFromPayload(payload map[string]any, finalURL, continueURL string) route {
	pageType := ""
	if page, ok := payload["page"].(map[string]any); ok {
		pageType = stringFromAny(page["type"])
	}
	if pageType == "" {
		pageType = inferPageType(finalURL)
	}
	return route{PageType: pageType, ContinueURL: continueURL, FinalURL: finalURL}
}

func (d *Driver) securityHeaders(deviceID, referer, flow string, includeSentinel bool, email string) http.Header {
	headers := header(map[string]string{
		"Accept":        "application/json",
		"Origin":        authBaseURL,
		"Referer":       referer,
		"OAI-Device-Id": deviceID,
	})
	if includeSentinel {
		token := oauth.BuildSentinelToken(d.session, deviceID, flow, d.userAgent, primaryLocale(d.locale))
		if token != "" {
			headers.Set("OpenAI-Sentinel-Token", token)
		}
	}
	for key, values := range oauth.DatadogTraceHeaders() {
		for _, value := range values {
			headers.Add(key, value)
		}
	}
	return headers
}

func (d *Driver) warmSession() error {
	res, err := d.session.Get(baseURL+"/", nil, header(map[string]string{"Referer": baseURL + "/"}), true)
	if err != nil {
		return err
	}
	if err := res.RaiseForStatus(); err != nil {
		return err
	}
	d.maybeSeedDeviceID(res)
	return nil
}

func (d *Driver) ensureDeviceID() string {
	if d.deviceID != "" {
		return d.deviceID
	}
	for _, name := range []string{"oai-did", "oaiDid", "oai_did"} {
		if value := d.session.CookieValue(authBaseURL, name); value != "" {
			d.deviceID = value
			return value
		}
	}
	d.deviceID = randomUUIDish()
	d.session.SetCookie(authBaseURL, "oai-did", d.deviceID)
	return d.deviceID
}

func (d *Driver) ensureAuthSessionLoggingID() string {
	if d.authSessionLoggingID == "" {
		d.authSessionLoggingID = randomUUIDish()
	}
	return d.authSessionLoggingID
}

func (d *Driver) maybeSeedDeviceID(res *oauth.HTTPResult) {
	deviceID := extractDeviceID(res.Text())
	if deviceID == "" {
		deviceID = extractCookieDeviceID(res.Headers.Get("Set-Cookie"))
	}
	if deviceID == "" {
		return
	}
	d.deviceID = deviceID
	d.session.SetCookie(authBaseURL, "oai-did", deviceID)
}

func (d *Driver) resolveContinueURL(res *oauth.HTTPResult, payload map[string]any, scope string) string {
	if candidate := normalizeURL(stringFromAny(payload["continue_url"])); candidate != "" {
		return candidate
	}
	if candidate := normalizeURL(res.Headers.Get("Location")); candidate != "" {
		return candidate
	}
	if candidate := extractContinueURL(res.Text()); candidate != "" {
		return candidate
	}
	return normalizeURL(d.continueURLCache[scope])
}

func (d *Driver) rememberContinueURL(scope, continueURL string) {
	if continueURL == "" {
		return
	}
	if isSafeContinueURL(continueURL) {
		d.continueURLCache[scope] = continueURL
		return
	}
	delete(d.continueURLCache, scope)
}

func (d *Driver) rememberArtifacts(scope string, payload map[string]any, continueURL string) {
	cached := map[string]string{}
	for key, value := range d.artifactCache[scope] {
		cached[key] = value
	}
	if page, ok := payload["page"].(map[string]any); ok {
		if pageType := stringFromAny(page["type"]); pageType != "" {
			cached["page_type"] = pageType
		}
	}
	if continueURL != "" {
		cached["continue_url"] = continueURL
	}
	for _, key := range []string{"error", "error_code"} {
		if value := stringFromAny(payload[key]); value != "" {
			cached[key] = value
		}
	}
	if scope == "submit_profile" {
		for key, value := range d.createAccountCookieArtifacts() {
			cached[key] = value
		}
	}
	if len(cached) > 0 {
		d.artifactCache[scope] = cached
	}
}

func (d *Driver) mergeArtifacts(scope string, payload map[string]any) map[string]any {
	cached := d.artifactCache[scope]
	if len(cached) == 0 {
		return payload
	}
	merged := map[string]any{}
	for key, value := range payload {
		merged[key] = value
	}
	page, _ := merged["page"].(map[string]any)
	if page == nil {
		page = map[string]any{}
	}
	if stringFromAny(page["type"]) == "" && cached["page_type"] != "" {
		page["type"] = cached["page_type"]
		merged["page"] = page
	}
	if stringFromAny(merged["continue_url"]) == "" && cached["continue_url"] != "" {
		merged["continue_url"] = cached["continue_url"]
	}
	return merged
}

func (d *Driver) cachedArtifact(scope, key string) string {
	if d.artifactCache[scope] == nil {
		return ""
	}
	return d.artifactCache[scope][key]
}

func (d *Driver) createAccountCookieArtifacts() map[string]string {
	raw := d.session.CookieValue(authBaseURL, "oai-client-auth-session")
	payload := decodeCookiePayload(raw)
	if payload == nil {
		return nil
	}
	out := map[string]string{}
	if workspaceID := extractPersonalWorkspaceID(payload); workspaceID != "" {
		out["workspace_id"] = workspaceID
	}
	if accountID := extractAccountID(payload); accountID != "" {
		out["account_id"] = accountID
	}
	return out
}

func classify(r route) routeState {
	pageType := strings.ToLower(strings.TrimSpace(r.PageType))
	continueURL := strings.ToLower(strings.TrimSpace(r.ContinueURL))
	finalURL := strings.ToLower(strings.TrimSpace(r.FinalURL))
	if containsAny(pageType, "error") || containsAny(continueURL, "/error") || containsAny(finalURL, "/error") {
		return stateError
	}
	if containsAny(pageType, "authorize_landing") {
		return stateLanding
	}
	if containsAny(pageType, "password", "create_account_password") || containsAny(continueURL, "/create-account/password", "/u/signup/password") || containsAny(finalURL, "/create-account/password", "/u/signup/password") {
		return statePassword
	}
	if containsAny(pageType, "otp", "email_verification") || containsAny(continueURL, "email-verification", "email-otp") || containsAny(finalURL, "email-verification", "email-otp") {
		return stateEmailOTP
	}
	if containsAny(pageType, "about_you") || containsAny(continueURL, "/about-you", "/u/signup/information") || containsAny(finalURL, "/about-you", "/u/signup/information") {
		return stateAboutYou
	}
	if looksSessionReady(continueURL) || looksSessionReady(finalURL) {
		return stateSession
	}
	return stateUnknown
}

func inferPageType(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(value, "/error"):
		return "error"
	case strings.Contains(value, "/create-account/password") || strings.Contains(value, "/u/signup/password"):
		return "create_account_password"
	case strings.Contains(value, "/email-verification") || strings.Contains(value, "/email-otp"):
		return "email_verification"
	case strings.Contains(value, "/about-you") || strings.Contains(value, "/u/signup/information"):
		return "about_you"
	case strings.Contains(value, "chatgpt.com"):
		return "session_ready"
	default:
		return ""
	}
}

func looksSessionReady(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(value, "chatgpt.com") && !strings.Contains(value, "auth.openai.com") && !strings.Contains(value, "/auth/")
}

func safeJSON(res *oauth.HTTPResult) map[string]any {
	var payload map[string]any
	if err := res.JSON(&payload); err == nil && payload != nil {
		return payload
	}
	return map[string]any{}
}

func normalizeURL(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\/`, "/"))
	if value == "" {
		return ""
	}
	if decoded, err := url.QueryUnescape(value); err == nil {
		value = decoded
	}
	if strings.HasPrefix(value, "/") {
		return authBaseURL + value
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return ""
}

func isSafeContinueURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	path := strings.ToLower(strings.TrimSpace(parsed.Path))
	return path != "/about-you" && path != "/add-phone" && path != "/error"
}

func extractContinueURL(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`"continue_url"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`'continue_url'\s*:\s*'([^']+)'`),
		regexp.MustCompile(`continue_url=([^&"'\s>]+)`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(text); len(match) > 1 {
			if normalized := normalizeURL(match[1]); normalized != "" {
				return normalized
			}
		}
	}
	return ""
}

func extractDeviceID(text string) string {
	match := regexp.MustCompile(`(?i)(?:oai[-_]?did|oaiDid)["'\\s:=]+([a-f0-9-]{36})`).FindStringSubmatch(text)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractCookieDeviceID(value string) string {
	match := regexp.MustCompile(`(?i)(?:^|[,;\s])oai-did=([a-f0-9-]{36})(?:$|[,;\s])`).FindStringSubmatch(value)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func parseRetryAfter(res *oauth.HTTPResult, fallback float64) time.Duration {
	value := strings.TrimSpace(res.Headers.Get("Retry-After"))
	if value != "" {
		if parsed, err := time.ParseDuration(value + "s"); err == nil && parsed > 0 {
			if parsed > 120*time.Second {
				return 120 * time.Second
			}
			return parsed
		}
	}
	if fallback < 1 {
		fallback = 1
	}
	return time.Duration(fallback * float64(time.Second))
}

func waitForCode(ctx context.Context, email string, timeout time.Duration, fn func(string) (string, error)) (string, error) {
	type out struct {
		code string
		err  error
	}
	ch := make(chan out, 1)
	go func() {
		code, err := fn(email)
		ch <- out{code: code, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("任务已取消: %w", ctx.Err())
	case <-time.After(timeout):
		return "", fmt.Errorf("等待验证码超时（%s）", timeout)
	case item := <-ch:
		if item.err != nil {
			return "", item.err
		}
		code := strings.TrimSpace(item.code)
		if code == "" {
			return "", fmt.Errorf("验证码为空")
		}
		return code, nil
	}
}

func decodeCookiePayload(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	candidates := []string{raw}
	if idx := strings.Index(raw, "."); idx > 0 {
		candidates = append([]string{raw[:idx]}, candidates...)
	}
	for _, candidate := range candidates {
		payload, err := base64.RawURLEncoding.DecodeString(candidate)
		if err != nil {
			payload, err = base64.StdEncoding.DecodeString(candidate + strings.Repeat("=", (4-len(candidate)%4)%4))
		}
		if err != nil {
			continue
		}
		var out map[string]any
		if json.Unmarshal(payload, &out) == nil {
			return out
		}
	}
	return nil
}

func extractPersonalWorkspaceID(payload map[string]any) string {
	workspaces, _ := payload["workspaces"].([]any)
	for _, item := range workspaces {
		workspace, _ := item.(map[string]any)
		if strings.EqualFold(stringFromAny(workspace["kind"]), "personal") {
			if id := stringFromAny(workspace["id"]); id != "" {
				return id
			}
		}
	}
	return ""
}

func extractAccountID(payload map[string]any) string {
	for _, key := range []string{"account_id", "chatgpt_account_id"} {
		if value := stringFromAny(payload[key]); value != "" {
			return value
		}
	}
	if auth, ok := payload["https://api.openai.com/auth"].(map[string]any); ok {
		for _, key := range []string{"chatgpt_account_id", "account_id"} {
			if value := stringFromAny(auth[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func jwtUserID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if userID := stringFromAny(auth["user_id"]); userID != "" {
			return userID
		}
	}
	return stringFromAny(claims["sub"])
}

func birthdateForEmail(email string) string {
	sum := 0
	for _, ch := range []byte(email) {
		sum += int(ch)
	}
	year := 1986 + sum%14
	month := 1 + (sum/7)%12
	day := 1 + (sum/13)%28
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func primaryLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if idx := strings.Index(locale, ","); idx > 0 {
		locale = strings.TrimSpace(locale[:idx])
	}
	return firstNonEmpty(locale, "en-US")
}

func header(values map[string]string) http.Header {
	h := http.Header{}
	for key, value := range values {
		h.Set(key, value)
	}
	return h
}

func randomUUIDish() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", mathrand.Uint32(), mathrand.Uint32()&0xffff, mathrand.Uint32()&0xffff, mathrand.Uint32()&0xffff, mathrand.Uint64())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func humanDelay(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	delay := mathrand.Intn(maxMs-minMs) + minMs
	time.Sleep(time.Duration(delay) * time.Millisecond)
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
