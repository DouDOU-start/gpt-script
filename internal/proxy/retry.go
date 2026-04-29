package proxy

import (
	"net/http"
	"strings"
)

func IsCloudflareChallenge(statusCode int, headers http.Header, bodyOrError string) bool {
	if strings.EqualFold(headers.Get("cf-mitigated"), "challenge") {
		return true
	}
	message := strings.ToLower(bodyOrError)
	return statusCode == http.StatusForbidden && (strings.Contains(message, "just a moment") || strings.Contains(message, "enable javascript and cookies to continue") || strings.Contains(message, "_cf_chl_opt")) || strings.Contains(message, "captcha_or_challenge")
}

func IsRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	fragments := []string{
		"curl: (28)", "curl: (35)", "curl: (52)", "curl: (55)", "curl: (56)",
		"timed out", "timeout", "tls connect error", "ssl routines", "wrong version number", "tls handshake",
		"connection closed abruptly", "connection reset", "connection refused", "failed to connect to browser",
		"cannot connect to browser", "could not connect to browser", "cannot find context with specified id",
		"no node with given id found", "node with given id does not belong to the document", "target closed",
		"session closed", "execution context was destroyed", "no route to host", "empty reply from server",
		"recv failure", "send failure", "proxy scheme detection failed",
	}
	for _, fragment := range fragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
