package dockerpool

import "strings"

type ProxyAuth struct {
	Username string
	Password string
}

// ParseProxyURL splits a proxy URL into a proxy address (without credentials)
// and optional basic auth credentials.
//
// Input examples:
// - http://user:pass@host:port
// - http://host:port
func ParseProxyURL(proxyURL string) (string, *ProxyAuth, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return "", nil, nil
	}

	protoEnd := 0
	if idx := strings.Index(proxyURL, "://"); idx > 0 {
		protoEnd = idx + 3
	}

	proto := proxyURL[:protoEnd]
	rest := proxyURL[protoEnd:]

	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		return proxyURL, nil, nil
	}

	authPart := rest[:atIdx]
	hostPart := rest[atIdx+1:]

	colonIdx := strings.Index(authPart, ":")
	var auth *ProxyAuth
	if colonIdx >= 0 {
		auth = &ProxyAuth{
			Username: authPart[:colonIdx],
			Password: authPart[colonIdx+1:],
		}
	} else {
		auth = &ProxyAuth{Username: authPart}
	}

	return proto + hostPart, auth, nil
}
