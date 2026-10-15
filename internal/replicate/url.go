package replicate

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrSecretInURL is returned for URLs that carry credentials.
var ErrSecretInURL = errors.New("учетные данные в URL запрещены: укажите логин и токен в учетных данных target'а")

var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/\s][^\s]*$`)

// ValidateURL checks the URL of a target: https://..., ssh://... or
// user@host:path, without a password or token inside.
func ValidateURL(raw string) error {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return errors.New("URL не должен быть пустым и содержать пробелы")
	}
	if strings.HasPrefix(raw, "-") {
		return errors.New("URL не может начинаться с '-'")
	}
	if !strings.Contains(raw, "://") {
		if scpLike.MatchString(raw) {
			return nil
		}
		return errors.New("URL должен иметь вид https://..., ssh://... или user@host:path")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("некорректный URL: " + err.Error())
	}
	if u.Host == "" || u.Path == "" || u.Path == "/" {
		return errors.New("в URL должны быть хост и путь к репозиторию")
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
		return errors.New("поддерживаются только схемы https и ssh, а также вид user@host:path")
	}
	return nil
}
