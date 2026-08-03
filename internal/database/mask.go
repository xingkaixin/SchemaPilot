package database

import (
	"net/url"
	"strings"
)

const maskedPassword = "***"

// MaskDSN removes credentials from URL-style and MySQL-style DSNs.
func MaskDSN(raw string) string {
	if raw == "" {
		return ""
	}

	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		maskURLQuery(parsed)
		if parsed.User != nil {
			username := parsed.User.Username()
			if _, hasPassword := parsed.User.Password(); hasPassword {
				parsed.User = url.UserPassword(username, maskedPassword)
			}
		}
		return parsed.String()
	}

	return maskMySQLDSN(raw)
}

func maskURLQuery(parsed *url.URL) {
	query := parsed.Query()
	changed := false
	for key, values := range query {
		if !strings.EqualFold(key, "password") && !strings.EqualFold(key, "pass") {
			continue
		}
		for index := range values {
			values[index] = maskedPassword
		}
		query[key] = values
		changed = true
	}
	if changed {
		parsed.RawQuery = query.Encode()
	}
}

func maskMySQLDSN(raw string) string {
	at := strings.IndexByte(raw, '@')
	if at < 0 {
		return raw
	}

	credentials := raw[:at]
	separator := strings.IndexByte(credentials, ':')
	if separator < 0 {
		return raw
	}

	return credentials[:separator+1] + maskedPassword + raw[at:]
}
