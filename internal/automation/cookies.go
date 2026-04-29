package automation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-rod/rod/lib/proto"
)

// Cookie is a simplified cookie model to store in SQLite and restore later.
//
// It is intentionally compatible with CDP's Network cookie model.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

func (b *Browser) GetCookies() ([]Cookie, error) {
	if b.page == nil {
		return nil, fmt.Errorf("page not initialized")
	}
	res, err := proto.NetworkGetAllCookies{}.Call(b.page)
	if err != nil {
		return nil, err
	}
	out := make([]Cookie, 0, len(res.Cookies))
	for _, c := range res.Cookies {
		if c == nil {
			continue
		}
		out = append(out, Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			SameSite: string(c.SameSite),
		})
	}
	return out, nil
}

func (b *Browser) SetCookies(cookies []Cookie) error {
	if b.page == nil {
		if _, err := b.NewPage(); err != nil {
			return err
		}
	}
	for _, c := range cookies {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		req := proto.NetworkSetCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
		}
		if strings.TrimSpace(c.SameSite) != "" {
			req.SameSite = proto.NetworkCookieSameSite(c.SameSite)
		}
		if c.Expires > 0 {
			req.Expires = proto.TimeSinceEpoch(c.Expires)
		}
		if _, err := req.Call(b.page); err != nil {
			// best-effort: continue setting other cookies
			continue
		}
	}
	return nil
}

func (b *Browser) ClearOpenAICookies() error {
	if b.page == nil {
		if _, err := b.NewPage(); err != nil {
			return err
		}
	}

	cookies, err := b.GetCookies()
	if err != nil {
		return err
	}

	for _, c := range cookies {
		if !isOpenAICookieDomain(c.Domain) {
			continue
		}
		path := c.Path
		if strings.TrimSpace(path) == "" {
			path = "/"
		}
		if err := (proto.NetworkDeleteCookies{
			Name:   c.Name,
			Domain: c.Domain,
			Path:   path,
		}).Call(b.page); err != nil {
			return err
		}
	}

	return nil
}

func isOpenAICookieDomain(domain string) bool {
	d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if d == "" {
		return false
	}
	return d == "chatgpt.com" || strings.HasSuffix(d, ".chatgpt.com") ||
		d == "openai.com" || strings.HasSuffix(d, ".openai.com") ||
		d == "auth.openai.com" || d == "accounts.openai.com" || d == "chat.openai.com"
}

func CookiesToJSON(cookies []Cookie) (string, error) {
	b, err := json.Marshal(cookies)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func CookiesFromJSON(raw string) ([]Cookie, error) {
	var cookies []Cookie
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(raw), &cookies); err != nil {
		return nil, err
	}
	return cookies, nil
}
