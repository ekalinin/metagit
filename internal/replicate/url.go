package replicate

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrSecretInURL is returned for URLs that carry credentials.
var ErrSecretInURL = errors.New("credentials in the URL are not allowed: set the username and token in the target credentials")

var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/\s][^\s]*$`)

// ValidateURL checks the URL of a target: https://..., ssh://... or
// user@host:path, without a password or token inside.
func ValidateURL(raw string) error {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return errors.New("URL must not be empty or contain spaces")
	}
	if strings.HasPrefix(raw, "-") {
		return errors.New("URL must not start with '-'")
	}
	if !strings.Contains(raw, "://") {
		if scpLike.MatchString(raw) {
			return nil
		}
		return errors.New("URL must look like https://..., ssh://... or user@host:path")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid URL: " + err.Error())
	}
	if u.Host == "" || u.Path == "" || u.Path == "/" {
		return errors.New("URL must contain a host and a repository path")
	}
	switch u.Scheme {
	case "https":
		if u.User != nil {
			return ErrSecretInURL
		}
	case "ssh":
		if _, hasPassword := u.User.Password(); hasPassword {
			return ErrSecretInURL
		}
	default:
		return errors.New("only the https and ssh schemes and the user@host:path form are supported")
	}
	return nil
}
