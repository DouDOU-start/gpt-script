package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	ClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	Scope            = "openid profile email offline_access"
	AuthBaseURL      = "https://auth.openai.com"
	CallbackURL      = "http://localhost:1455/auth/callback"
	defaultLocale    = "en-US,en;q=0.9"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.7001.90 Safari/537.36"
)

type Account struct {
	Email          string
	Password       string
	WorkspaceID    string
	OrganizationID string
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

type TokenResult struct {
	AccessToken      string         `json:"access_token,omitempty"`
	RefreshToken     string         `json:"refresh_token,omitempty"`
	IDToken          string         `json:"id_token,omitempty"`
	ExpiresIn        int            `json:"expires_in,omitempty"`
	ExpiresAt        time.Time      `json:"expires_at,omitempty"`
	Scope            string         `json:"scope,omitempty"`
	TokenType        string         `json:"token_type,omitempty"`
	ChatGPTAccountID string         `json:"chatgpt_account_id,omitempty"`
	ChatGPTUserID    string         `json:"chatgpt_user_id,omitempty"`
	OrganizationID   string         `json:"organization_id,omitempty"`
	PlanType         string         `json:"plan_type,omitempty"`
	Raw              map[string]any `json:"raw,omitempty"`
}

type route struct {
	PageType    string
	ContinueURL string
	FinalURL    string
}

type state string

const (
	stateAuthorizeBootstrap state = "authorize_bootstrap"
	statePassword           state = "password_page"
	stateAddPhone           state = "add_phone"
	stateError              state = "error_page"
	stateOTP                state = "oauth_otp_page"
	stateOTPRejected        state = "oauth_otp_rejected"
	stateConsent            state = "consent"
	stateWorkspace          state = "workspace_select"
	stateOrganization       state = "organization_select"
	stateOAuth2             state = "oauth2_auth"
	stateCallback           state = "callback"
	stateTokenExchange      state = "token_exchange"
	stateUnknown            state = "unknown"
)

type Driver struct {
	session             *Session
	userAgent           string
	locale              string
	state               string
	verifier            string
	callbackCode        string
	oauth2URL           string
	consentURL          string
	deviceID            string
	passwordReferer     string
	organizationPayload map[string]any
	lastRoute           route
}

func Run(ctx context.Context, account Account, opts Options) (*TokenResult, error) {
	account.Email = strings.TrimSpace(account.Email)
	if account.Email == "" || account.Password == "" {
		return nil, fmt.Errorf("邮箱和密码不能为空")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.CodeTimeout <= 0 {
		opts.CodeTimeout = 2 * time.Minute
	}
	userAgent := firstNonEmpty(opts.UserAgent, defaultUserAgent)
	locale := firstNonEmpty(opts.Locale, defaultLocale)
	sess, err := NewSession(opts.ProxyURL, userAgent, locale, opts.Timeout)
	if err != nil {
		return nil, err
	}
	d := &Driver{session: sess, userAgent: userAgent, locale: locale}
	progress := func(detail string) {
		if opts.Progress != nil {
			opts.Progress(detail)
		} else {
			log.Printf("[OAuth] %s", detail)
		}
	}

	progress("bootstrapping oauth authorize flow")
	rt, err := d.authorizeBootstrap(account)
	if err != nil {
		return nil, err
	}
	for steps := 0; steps < 20; steps++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("任务已取消: %w", ctx.Err())
		default:
		}
		s := classifyRoute(rt)
		switch s {
		case statePassword:
			progress("submitting oauth password")
			rt, err = d.submitPassword(account)
		case stateOTP:
			progress("waiting for oauth email verification code")
			if opts.VerificationCode == nil {
				return nil, fmt.Errorf("需要邮箱验证码，但未设置验证码获取回调")
			}
			code, codeErr := opts.VerificationCode(account.Email)
			if codeErr != nil {
				return nil, codeErr
			}
			progress("submitting oauth email verification code")
			rt, err = d.submitEmailCode(account, code)
		case stateOTPRejected:
			progress("oauth email verification code was rejected; requesting another code")
			if err = d.resendEmailCode(); err != nil {
				return nil, err
			}
			rt = route{PageType: "email_otp_step", ContinueURL: AuthBaseURL + "/email-verification"}
		case stateConsent, stateWorkspace:
			progress("submitting workspace selection")
			rt, err = d.submitWorkspace(account)
		case stateOrganization:
			progress("submitting organization selection")
			rt, err = d.submitOrganization(account)
		case stateOAuth2, stateCallback:
			progress("following oauth redirect chain")
			rt, err = d.followOAuth2Auth()
			if err == nil && classifyRoute(rt) == stateCallback {
				progress("exchanging oauth token")
				return d.exchangeToken(ctx)
			}
		case stateTokenExchange:
			progress("exchanging oauth token")
			return d.exchangeToken(ctx)
		case stateAddPhone:
			return nil, fmt.Errorf("oauth flow hit add_phone")
		case stateError:
			return nil, fmt.Errorf("oauth flow hit error_page")
		case stateAuthorizeBootstrap:
			progress("following oauth redirect chain")
			rt, err = d.followOAuth2Auth()
		default:
			return nil, fmt.Errorf("oauth flow reached unsupported state: %s", s)
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("oauth flow exceeded maximum steps")
}

func (d *Driver) authorizeBootstrap(account Account) (route, error) {
	d.reset()
	stateValue, err := randomURLToken(16)
	if err != nil {
		return route{}, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return route{}, err
	}
	d.state = stateValue
	d.verifier = verifier
	humanDelay(400, 1000)
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", ClientID)
	params.Set("redirect_uri", CallbackURL)
	params.Set("scope", Scope)
	params.Set("state", d.state)
	params.Set("code_challenge", pkceChallenge(d.verifier))
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "login")
	params.Set("login_hint", account.Email)
	params.Set("screen_hint", "login_or_signup")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	res, err := d.session.Get(AuthBaseURL+"/oauth/authorize", params, nil, true)
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	d.passwordReferer = res.URL
	return d.postAuthorizeContinue(account.Email, d.ensureDeviceID(), d.passwordReferer)
}

func (d *Driver) submitPassword(account Account) (route, error) {
	humanDelay(1000, 2500)
	headers := d.securityHeaders(firstNonEmpty(d.passwordReferer, AuthBaseURL+"/log-in/password"), "password_verify", d.ensureDeviceID())
	res, err := d.session.PostJSON(AuthBaseURL+"/api/accounts/password/verify", map[string]any{"password": account.Password}, headers)
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res)
}

func (d *Driver) submitEmailCode(account Account, code string) (route, error) {
	humanDelay(600, 1500)
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Origin", AuthBaseURL)
	res, err := d.session.PostJSON(AuthBaseURL+"/api/accounts/email-otp/validate", map[string]any{"code": strings.TrimSpace(code)}, headers)
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res)
}

func (d *Driver) resendEmailCode() error {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Origin", AuthBaseURL)
	res, err := d.session.PostJSON(AuthBaseURL+"/api/accounts/email-otp/resend", map[string]any{}, headers)
	if err != nil {
		return err
	}
	return res.RaiseForStatus()
}

func (d *Driver) submitWorkspace(account Account) (route, error) {
	humanDelay(500, 1200)
	workspaceID := strings.TrimSpace(account.WorkspaceID)
	if workspaceID == "" {
		workspaceID = d.resolvePersonalWorkspaceID()
	}
	if workspaceID == "" {
		return route{}, fmt.Errorf("oauth personal workspace id missing")
	}
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Origin", AuthBaseURL)
	res, err := d.session.PostJSON(AuthBaseURL+"/api/accounts/workspace/select", map[string]any{"workspace_id": workspaceID}, headers)
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res)
}

func (d *Driver) submitOrganization(account Account) (route, error) {
	body := d.organizationBody(account)
	if len(body) == 0 {
		return route{}, fmt.Errorf("oauth organization id missing")
	}
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Origin", AuthBaseURL)
	res, err := d.session.PostJSON(AuthBaseURL+"/api/accounts/organization/select", body, headers)
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res)
}

func (d *Driver) followOAuth2Auth() (route, error) {
	authorizeURL := firstNonEmpty(d.oauth2URL, AuthBaseURL+"/oauth2/authorize")
	currentURL := authorizeURL
	var referer string
	params := url.Values{}
	if d.oauth2URL == "" {
		params.Set("redirect_uri", CallbackURL)
	}
	for i := 0; i < 10; i++ {
		headers := http.Header{}
		if referer != "" {
			headers.Set("Referer", referer)
		}
		res, err := d.session.Get(currentURL, params, headers, false)
		if err != nil {
			return route{}, err
		}
		if err := res.RaiseForStatus(); err != nil {
			return route{}, err
		}
		location := strings.TrimSpace(res.Headers.Get("Location"))
		if location != "" {
			if parsed := resolveLocation(currentURL, location); parsed != "" {
				location = parsed
			}
			status := d.maybeCaptureCallback(location)
			if status == "ok" {
				return route{ContinueURL: authorizeURL, FinalURL: location}, nil
			}
			if status == "state_missing" || status == "state_mismatch" || (status == "origin_mismatch" && d.looksLikeCallback(location)) {
				return route{}, fmt.Errorf("oauth callback %s", strings.TrimPrefix(status, "state_"))
			}
			referer = currentURL
			currentURL = location
			params = nil
			continue
		}
		status := d.maybeCaptureCallback(res.URL)
		if status == "ok" {
			return route{ContinueURL: authorizeURL, FinalURL: res.URL}, nil
		}
		return route{ContinueURL: authorizeURL, FinalURL: res.URL}, nil
	}
	return route{}, fmt.Errorf("oauth2 auth redirect limit exceeded")
}

func (d *Driver) exchangeToken(ctx context.Context) (*TokenResult, error) {
	if strings.TrimSpace(d.callbackCode) == "" {
		return nil, fmt.Errorf("oauth callback code missing")
	}
	if strings.TrimSpace(d.verifier) == "" {
		return nil, fmt.Errorf("oauth pkce verifier missing")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ClientID)
	form.Set("redirect_uri", CallbackURL)
	form.Set("code", d.callbackCode)
	form.Set("code_verifier", d.verifier)
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Origin", AuthBaseURL)
	resCh := make(chan struct {
		res *HTTPResult
		err error
	}, 1)
	go func() {
		res, err := d.session.PostForm(AuthBaseURL+"/oauth/token", form, headers)
		resCh <- struct {
			res *HTTPResult
			err error
		}{res: res, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("任务已取消: %w", ctx.Err())
	case item := <-resCh:
		if item.err != nil {
			return nil, item.err
		}
		if err := item.res.RaiseForStatus(); err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := item.res.JSON(&payload); err != nil {
			return nil, err
		}
		return enrichTokenPayload(payload), nil
	}
}

func (d *Driver) postAuthorizeContinue(email, deviceID, referer string) (route, error) {
	headers := d.securityHeaders(referer, "authorize_continue", deviceID)
	payload := map[string]any{"username": map[string]any{"kind": "email", "value": email}}
	res, err := d.session.PostJSON(AuthBaseURL+"/api/accounts/authorize/continue", payload, headers)
	if err != nil {
		return route{}, err
	}
	if err := res.RaiseForStatus(); err != nil {
		return route{}, err
	}
	return d.routeFromPayloadResponse(res)
}

func (d *Driver) routeFromPayloadResponse(res *HTTPResult) (route, error) {
	var payload map[string]any
	if err := res.JSON(&payload); err != nil {
		return route{}, err
	}
	continueURL := stringFromAny(payload["continue_url"])
	d.rememberOAuth2URL(continueURL)
	finalURL := firstNonEmpty(continueURL, res.URL)
	status := d.maybeCaptureCallback(continueURL)
	if status == "state_missing" || status == "state_mismatch" || (status == "origin_mismatch" && d.looksLikeCallback(continueURL)) {
		return route{}, fmt.Errorf("oauth callback %s", strings.TrimPrefix(status, "state_"))
	}
	if continueURL != "" && !isOAuth2AuthURL(continueURL) && status != "ok" {
		follow, err := d.session.Get(continueURL, nil, nil, true)
		if err != nil {
			return route{}, err
		}
		if err := follow.RaiseForStatus(); err != nil {
			return route{}, err
		}
		finalURL = follow.URL
	}
	d.rememberConsentURL(continueURL, finalURL)
	if data, ok := payload["data"].(map[string]any); ok {
		if orgs, ok := data["orgs"].([]any); ok && len(orgs) > 0 {
			d.organizationPayload = payload
		}
	}
	pageType := ""
	if page, ok := payload["page"].(map[string]any); ok {
		pageType = stringFromAny(page["type"])
	}
	r := route{PageType: pageType, ContinueURL: continueURL, FinalURL: finalURL}
	d.lastRoute = r
	return r, nil
}

func (d *Driver) securityHeaders(referer, flow, deviceID string) http.Header {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Origin", AuthBaseURL)
	headers.Set("Referer", referer)
	headers.Set("OAI-Device-Id", deviceID)
	headers.Set("OpenAI-Sentinel-Token", BuildSentinelToken(d.session, deviceID, flow, d.userAgent, d.locale))
	for key, values := range DatadogTraceHeaders() {
		for _, value := range values {
			headers.Add(key, value)
		}
	}
	return headers
}

func (d *Driver) ensureDeviceID() string {
	if d.deviceID != "" {
		return d.deviceID
	}
	deviceID := d.session.CookieValue(AuthBaseURL, "oai-did")
	if deviceID == "" {
		deviceID = randomToken(16)
		d.session.SetCookie(AuthBaseURL, "oai-did", deviceID)
	}
	d.deviceID = deviceID
	return deviceID
}

func (d *Driver) reset() {
	d.callbackCode = ""
	d.oauth2URL = ""
	d.consentURL = ""
	d.deviceID = ""
	d.passwordReferer = ""
	d.organizationPayload = nil
	d.lastRoute = route{}
}

func (d *Driver) rememberOAuth2URL(raw string) {
	if !isOAuth2AuthURL(raw) {
		return
	}
	d.oauth2URL = raw
	if st := queryValue(raw, "state"); st != "" {
		d.state = st
	}
}

func (d *Driver) rememberConsentURL(values ...string) {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), "consent") {
			d.consentURL = value
			return
		}
	}
}

func (d *Driver) maybeCaptureCallback(raw string) string {
	if !d.looksLikeCallback(raw) {
		return "not_callback"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "origin_mismatch"
	}
	expected, _ := url.Parse(CallbackURL)
	if parsed.Scheme != expected.Scheme || parsed.Host != expected.Host || parsed.Path != expected.Path {
		return "origin_mismatch"
	}
	callbackState := queryValue(raw, "state")
	if callbackState == "" {
		return "state_missing"
	}
	if d.state != "" && callbackState != d.state {
		return "state_mismatch"
	}
	if code := queryValue(raw, "code"); code != "" {
		d.callbackCode = code
	}
	if d.state == "" {
		d.state = callbackState
	}
	return "ok"
}

func (d *Driver) looksLikeCallback(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	expected, _ := url.Parse(CallbackURL)
	return parsed.Path == expected.Path && parsed.Query().Get("code") != ""
}

func (d *Driver) resolvePersonalWorkspaceID() string {
	if id := d.workspaceIDFromSessionCookie(); id != "" {
		return id
	}
	if d.consentURL == "" {
		return ""
	}
	res, err := d.session.Get(d.consentURL, nil, nil, true)
	if err != nil || res.RaiseForStatus() != nil {
		return ""
	}
	return extractPersonalWorkspaceID(res.Text())
}

func (d *Driver) workspaceIDFromSessionCookie() string {
	raw := d.session.CookieValue(AuthBaseURL, "oai-client-auth-session")
	if raw == "" {
		return ""
	}
	for _, candidate := range []string{raw, strings.SplitN(raw, ".", 2)[0]} {
		payload, err := base64.RawURLEncoding.DecodeString(candidate)
		if err != nil {
			payload, err = base64.StdEncoding.DecodeString(candidate + strings.Repeat("=", (4-len(candidate)%4)%4))
		}
		if err != nil {
			continue
		}
		var data map[string]any
		if json.Unmarshal(payload, &data) != nil {
			continue
		}
		if clientID := stringFromAny(data["openai_client_id"]); clientID != "" && clientID != ClientID {
			continue
		}
		if mode := strings.ToLower(stringFromAny(data["email_verification_mode"])); mode == "onboarding" {
			continue
		}
		if data["signup_mode"] != nil || data["passwordless_otp_from_password_redirect"] != nil {
			continue
		}
		if workspaces, ok := data["workspaces"].([]any); ok {
			for _, item := range workspaces {
				ws, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if strings.EqualFold(stringFromAny(ws["kind"]), "personal") {
					return strings.TrimSpace(stringFromAny(ws["id"]))
				}
			}
		}
	}
	return ""
}

func (d *Driver) organizationBody(account Account) map[string]string {
	if strings.TrimSpace(account.OrganizationID) != "" {
		return map[string]string{"org_id": strings.TrimSpace(account.OrganizationID)}
	}
	payload := d.organizationPayload
	if payload == nil {
		return nil
	}
	data, _ := payload["data"].(map[string]any)
	orgs, _ := data["orgs"].([]any)
	if len(orgs) == 0 {
		return nil
	}
	first, _ := orgs[0].(map[string]any)
	orgID := stringFromAny(first["id"])
	if orgID == "" {
		return nil
	}
	body := map[string]string{"org_id": orgID}
	projects, _ := first["projects"].([]any)
	if len(projects) > 0 {
		project, _ := projects[0].(map[string]any)
		if projectID := stringFromAny(project["id"]); projectID != "" {
			body["project_id"] = projectID
		}
	}
	return body
}

func classifyRoute(r route) state {
	pageType := strings.ToLower(strings.TrimSpace(r.PageType))
	continueURL := strings.ToLower(strings.TrimSpace(r.ContinueURL))
	finalURL := strings.ToLower(strings.TrimSpace(r.FinalURL))
	if containsAny(pageType, "error", "oauth_error", "error_page") || containsAny(continueURL, "/error") || containsAny(finalURL, "/error") {
		return stateError
	}
	if containsAny(pageType, "otp_rejected", "email_otp_rejected", "incorrect_code") {
		return stateOTPRejected
	}
	if containsAny(pageType, "password", "login_password", "username_password") || containsAny(continueURL, "/log-in/password", "/password") || containsAny(finalURL, "/log-in/password", "/password") {
		return statePassword
	}
	if containsAny(pageType, "phone") || containsAny(continueURL, "/add-phone", "phone") || containsAny(finalURL, "/add-phone", "phone") {
		return stateAddPhone
	}
	if containsAny(pageType, "otp") || containsAny(continueURL, "otp", "email-verification") || containsAny(finalURL, "otp", "email-verification") {
		return stateOTP
	}
	if finalURL != "" && containsAny(finalURL, "/auth/callback", "callback?", "callback/") {
		return stateCallback
	}
	if finalURL != "" && containsAny(finalURL, "/token_exchange", "/oauth/token", "token?", "token/") {
		return stateTokenExchange
	}
	if continueURL != "" && containsAny(continueURL, "/oauth/authorize", "/oauth2/authorize", "/oauth2/auth") {
		return stateOAuth2
	}
	if containsAny(pageType, "consent") || containsAny(continueURL, "consent") || containsAny(finalURL, "consent") {
		return stateConsent
	}
	if containsAny(pageType, "workspace") || containsAny(continueURL, "workspace") || containsAny(finalURL, "workspace") {
		return stateWorkspace
	}
	if containsAny(pageType, "organization") || containsAny(continueURL, "organization") || containsAny(finalURL, "organization") {
		return stateOrganization
	}
	if containsAny(continueURL, "/authorize", "/oauth/continue", "/login") || containsAny(finalURL, "/authorize", "/oauth/continue", "/login") {
		return stateAuthorizeBootstrap
	}
	return stateUnknown
}

func enrichTokenPayload(payload map[string]any) *TokenResult {
	accessToken := stringFromAny(payload["access_token"])
	idToken := stringFromAny(payload["id_token"])
	accessClaims := decodeJWTPayload(accessToken)
	idClaims := decodeJWTPayload(idToken)
	accessIdentity := extractOpenAIIdentity(accessClaims)
	idIdentity := extractOpenAIIdentity(idClaims)
	expiresIn := intFromAny(payload["expires_in"])
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return &TokenResult{
		AccessToken:      accessToken,
		RefreshToken:     stringFromAny(payload["refresh_token"]),
		IDToken:          idToken,
		ExpiresIn:        expiresIn,
		ExpiresAt:        time.Now().UTC().Add(time.Duration(expiresIn) * time.Second),
		Scope:            stringFromAny(payload["scope"]),
		TokenType:        stringFromAny(payload["token_type"]),
		ChatGPTAccountID: firstNonEmpty(idIdentity["chatgpt_account_id"], accessIdentity["chatgpt_account_id"]),
		ChatGPTUserID:    firstNonEmpty(idIdentity["chatgpt_user_id"], accessIdentity["chatgpt_user_id"]),
		OrganizationID:   firstNonEmpty(idIdentity["organization_id"], accessIdentity["organization_id"]),
		PlanType:         firstNonEmpty(planType(idClaims), planType(accessClaims)),
		Raw:              payload,
	}
}

func decodeJWTPayload(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 || parts[1] == "" {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(payload, &out) != nil {
		return nil
	}
	return out
}

func extractOpenAIIdentity(claims map[string]any) map[string]string {
	out := map[string]string{"chatgpt_account_id": "", "chatgpt_user_id": "", "organization_id": ""}
	if claims == nil {
		return out
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	out["chatgpt_account_id"] = stringFromAny(auth["chatgpt_account_id"])
	out["chatgpt_user_id"] = firstNonEmpty(stringFromAny(auth["chatgpt_user_id"]), stringFromAny(auth["user_id"]))
	out["organization_id"] = stringFromAny(auth["organization_id"])
	return out
}

func planType(claims map[string]any) string {
	if claims == nil {
		return ""
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	return firstNonEmpty(stringFromAny(auth["chatgpt_plan_type"]), stringFromAny(auth["plan_type"]), stringFromAny(auth["chatgpt_plan"]))
}

func extractPersonalWorkspaceID(html string) string {
	normalized := strings.ReplaceAll(html, `\"`, `"`)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\{[^{}]*?"id"\s*:?\s*"([^"]+)"[^{}]*?"kind"\s*:?\s*"personal"`),
		regexp.MustCompile(`\{[^{}]*?"kind"\s*:?\s*"personal"[^{}]*?"id"\s*:?\s*"([^"]+)"`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(normalized); len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomURLToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func randomToken(n int) string {
	v, err := randomURLToken(n)
	if err != nil {
		return randomStringFrom("abcdefghijklmnopqrstuvwxyz0123456789", n)
	}
	return v
}

func randomStringFrom(chars string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(chars[mathrand.Intn(len(chars))])
	}
	return b.String()
}

func humanDelay(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	time.Sleep(time.Duration(mathrand.Intn(maxMs-minMs)+minMs) * time.Millisecond)
}

func isOAuth2AuthURL(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return value != "" && containsAny(value, "/oauth/authorize", "/oauth2/authorize", "/oauth2/auth")
}

func resolveLocation(baseURL, location string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return location
	}
	ref, err := url.Parse(location)
	if err != nil {
		return location
	}
	return base.ResolveReference(ref).String()
}

func queryValue(raw, key string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get(key))
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

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}
