package browsersignup

import (
	"net/url"
	"strings"
)

// PlaywrightProxy is the proxy object accepted by Camoufox and Playwright.
type PlaywrightProxy struct {
	Server   string `json:"server"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func parsePlaywrightProxy(raw string) *PlaywrightProxy {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	out := &PlaywrightProxy{Server: scheme + "://" + u.Hostname() + ":" + port}
	if u.User != nil {
		out.Username = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			out.Password = pass
		}
	}
	return out
}
