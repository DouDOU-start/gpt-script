package oauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

type Session struct {
	client        *http.Client
	noRedirect    *http.Client
	defaultHeader http.Header
}

type HTTPResult struct {
	StatusCode int
	URL        string
	Headers    http.Header
	Body       []byte
	RequestURL string
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
}

func NewSession(proxyURL, userAgent, locale string, timeout time.Duration) (*Session, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyURL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(proxyURL))
		if err != nil {
			return nil, fmt.Errorf("代理地址无效: %w", err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	header := http.Header{}
	header.Set("User-Agent", strings.TrimSpace(userAgent))
	header.Set("Accept-Language", strings.TrimSpace(locale))
	if header.Get("User-Agent") == "" {
		header.Set("User-Agent", defaultUserAgent)
	}
	if header.Get("Accept-Language") == "" {
		header.Set("Accept-Language", "en-US,en;q=0.9")
	}
	client := &http.Client{Jar: jar, Transport: transport, Timeout: timeout}
	noRedirect := &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Session{client: client, noRedirect: noRedirect, defaultHeader: header}, nil
}

func (s *Session) Get(rawURL string, params url.Values, headers http.Header, followRedirects bool) (*HTTPResult, error) {
	if len(params) > 0 {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		for key, values := range params {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}
	return s.do(http.MethodGet, rawURL, nil, headers, followRedirects)
}

func (s *Session) PostJSON(rawURL string, payload any, headers http.Header) (*HTTPResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "application/json")
	return s.do(http.MethodPost, rawURL, bytes.NewReader(body), headers, true)
}

func (s *Session) PostForm(rawURL string, form url.Values, headers http.Header) (*HTTPResult, error) {
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(http.MethodPost, rawURL, strings.NewReader(form.Encode()), headers, true)
}

func (s *Session) PostText(rawURL string, body string, headers http.Header) (*HTTPResult, error) {
	return s.do(http.MethodPost, rawURL, strings.NewReader(body), headers, true)
}

func (s *Session) do(method, rawURL string, body io.Reader, headers http.Header, followRedirects bool) (*HTTPResult, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, err
	}
	for key, values := range s.defaultHeader {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	for key, values := range headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	applyBrowserHeaders(req)
	client := s.client
	if !followRedirects {
		client = s.noRedirect
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return &HTTPResult{
		StatusCode: res.StatusCode,
		URL:        res.Request.URL.String(),
		Headers:    res.Header.Clone(),
		Body:       payload,
		RequestURL: rawURL,
	}, nil
}

func (r *HTTPResult) JSON(out any) error {
	return json.Unmarshal(r.Body, out)
}

func (r *HTTPResult) Text() string {
	return string(r.Body)
}

func (r *HTTPResult) RaiseForStatus() error {
	if r.StatusCode < 400 {
		return nil
	}
	body := strings.Join(strings.Fields(string(r.Body)), " ")
	if len(body) > 240 {
		body = body[:237] + "..."
	}
	if body != "" {
		return fmt.Errorf("HTTP %d for %s: %s", r.StatusCode, r.RequestURL, body)
	}
	return fmt.Errorf("HTTP %d for %s", r.StatusCode, r.RequestURL)
}

func (s *Session) CookieValue(rawURL, name string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	for _, cookie := range s.client.Jar.Cookies(u) {
		if cookie.Name == name {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func (s *Session) SetCookie(rawURL, name, value string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	s.client.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

func (s *Session) CookieSnapshot(rawURLs ...string) []Cookie {
	seen := map[string]bool{}
	out := []Cookie{}
	for _, rawURL := range rawURLs {
		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		for _, c := range s.client.Jar.Cookies(u) {
			key := rawURL + "\x00" + c.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			path := c.Path
			if path == "" {
				path = "/"
			}
			out = append(out, Cookie{
				Name:     c.Name,
				Value:    c.Value,
				Domain:   u.Hostname(),
				Path:     path,
				HTTPOnly: c.HttpOnly,
				Secure:   c.Secure,
			})
		}
	}
	return out
}

func applyBrowserHeaders(req *http.Request) {
	ua := req.Header.Get("User-Agent")
	major := chromeMajor(ua)
	if req.Method == http.MethodGet {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Dest", "document")
	} else {
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
	}
	req.Header.Set("Sec-CH-UA", fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not.A/Brand";v="99"`, major, major))
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	if strings.Contains(ua, "Windows") {
		req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	} else if strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh") {
		req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	} else {
		req.Header.Set("Sec-CH-UA-Platform", `"Linux"`)
	}
}

func chromeMajor(ua string) string {
	idx := strings.Index(strings.ToLower(ua), "chrome/")
	if idx < 0 {
		return "146"
	}
	version := ua[idx+7:]
	if dot := strings.Index(version, "."); dot > 0 {
		return version[:dot]
	}
	return "146"
}
