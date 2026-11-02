package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limits of the site options of an app.
const (
	MaxExtraDomains = 10
	MaxWebUsers     = 10
	MaxUploadMB     = 10240
	MinReadTimeout  = 10
	MaxReadTimeout  = 3600
	MaxTitleLen     = 80
	MaxMessageLen   = 500
)

var webUserRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// ExtraDomain checks one more domain of an app and where it goes: an empty to means it serves the
// app, otherwise it redirects to the domain in to.
func ExtraDomain(name, to, mainDomain string) error {
	if name == "" {
		return errors.New("informe o domínio")
	}
	if err := Domain(name); err != nil {
		return err
	}
	if name == mainDomain {
		return errors.New("esse já é o domínio principal da aplicação")
	}
	if to == "" {
		return nil
	}
	if err := Domain(to); err != nil {
		return errors.New("destino do redirecionamento: " + err.Error())
	}
	if to == name {
		return errors.New("o destino do redirecionamento não pode ser o próprio domínio")
	}
	return nil
}

// WebUser checks a basic-auth user name: it ends up in the password file, so no ":" or spaces.
func WebUser(n string) error {
	if !webUserRe.MatchString(n) {
		return errors.New("usuário inválido: use letras, números, ponto, hífen e sublinhado (até 32 caracteres)")
	}
	return nil
}

// WebPassword checks a basic-auth password. bcrypt only reads the first 72 bytes, so longer
// ones are refused instead of silently cut.
func WebPassword(p string) error {
	if len(p) < 8 {
		return errors.New("a senha precisa de pelo menos 8 caracteres")
	}
	if len(p) > 72 {
		return errors.New("a senha pode ter no máximo 72 caracteres")
	}
	return nil
}

// UploadMB checks the maximum upload size in MB; 0 keeps the Nginx default.
func UploadMB(n int) error {
	if n < 0 || n > MaxUploadMB {
		return fmt.Errorf("tamanho máximo de upload: de 0 a %d MB", MaxUploadMB)
	}
	return nil
}

// ReadTimeout checks the time in seconds nginx waits for the app's answer; 0 keeps the default.
func ReadTimeout(n int) error {
	if n != 0 && (n < MinReadTimeout || n > MaxReadTimeout) {
		return fmt.Errorf("tempo limite: de %d a %d segundos", MinReadTimeout, MaxReadTimeout)
	}
	return nil
}

// MaintenanceText checks the title and message of the maintenance page. Both are escaped when the
// page is written, so any character is fine; only the size and NUL bytes are limited.
func MaintenanceText(title, text string) error {
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return fmt.Errorf("o título pode ter até %d caracteres", MaxTitleLen)
	}
	if utf8.RuneCountInString(text) > MaxMessageLen {
		return fmt.Errorf("a mensagem pode ter até %d caracteres", MaxMessageLen)
	}
	if strings.ContainsRune(title+text, 0) {
		return errors.New("a página só pode ter texto")
	}
	return nil
}
