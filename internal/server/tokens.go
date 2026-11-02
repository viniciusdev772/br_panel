package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/auth"
	"github.com/viniciusdev772/br_panel/internal/cli"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// tokenPrefix makes a leaked token recognisable (and greppable) as one of ours.
const tokenPrefix = "brp_"

// hashToken is what is stored: the secret has 256 random bits, so a plain SHA-256 is enough and
// lets the lookup be a single indexed query.
func hashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// hasBearer is true when the request says "Authorization: Bearer …"; any other scheme (or none)
// is an ordinary browser request.
func hasBearer(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	return len(h) >= len("Bearer ") && strings.EqualFold(h[:len("Bearer ")], "Bearer ")
}

// authToken checks the bearer secret against the stored hashes.
func (s *Server) authToken(r *http.Request) (store.Token, bool) {
	secret := strings.TrimSpace(r.Header.Get("Authorization")[len("Bearer "):])
	if !strings.HasPrefix(secret, tokenPrefix) || len(secret) > 128 {
		return store.Token{}, false
	}
	return s.store.TokenByHash(hashToken(secret))
}

// cliAuth lets two kinds of caller use the panel's routes without a browser session: requests from
// the root-only CLI socket (marked by cliHandler) and requests carrying a valid access token. Both
// get a throw-away session, so every state change still runs through protected(), validation and
// the same handlers as the web pages. A token is not an ambient credential like a cookie, so
// skipping the CSRF field for it is safe.
func (s *Server) cliAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasBearer(r) && !viaCLI(r) {
			ip := auth.ClientIP(r)
			if s.auth.Locked(ip) {
				writeJSONStatus(w, http.StatusTooManyRequests, map[string]string{"error": "muitas tentativas; aguarde um minuto"})
				return
			}
			tok, ok := s.authToken(r)
			if !ok {
				s.auth.Fail(ip)
				writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "token inválido ou revogado"})
				return
			}
			s.store.TouchToken(tok.ID)
			r = r.WithContext(context.WithValue(r.Context(), cliKey{}, true))
		}
		if viaCLI(r) {
			defer s.auth.Grant(r)()
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	out := s.store.ListTokens()
	if out == nil {
		out = []store.Token{}
	}
	writeJSON(w, out)
}

// handleTokenCreate makes a token and shows its secret once; only the hash is kept.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	err := validate.Name(name)
	if err == nil && len(s.store.ListTokens()) >= store.MaxTokens {
		err = errors.New("o limite é de " + strconv.Itoa(store.MaxTokens) + " tokens; revogue algum antes")
	}
	secret := tokenPrefix + auth.RandomToken(32)
	if err == nil {
		if _, err = s.store.CreateToken(name, hashToken(secret)); errors.Is(err, store.ErrExists) {
			err = errors.New("já existe um token com esse nome")
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderCLIPage(w, r, page{Error: "Token não criado: " + err.Error()})
		return
	}
	s.store.AddEvent("app", "cli", "Token de acesso criado: "+name)
	s.renderCLIPage(w, r, page{Flash: "Token criado. Copie agora: ele não será mostrado de novo.", NewToken: secret, NewTokenName: name})
}

func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = s.store.DeleteToken(id)
	}
	msg := "Token revogado."
	if err != nil {
		msg = "Falha: token não encontrado."
	} else {
		s.store.AddEvent("app", "cli", "Token de acesso revogado")
	}
	http.Redirect(w, r, "/cli?msg="+url.QueryEscape(msg)+"#acesso-remoto", http.StatusSeeOther)
}

// renderCLIPage fills the fields every render of /cli needs.
func (s *Server) renderCLIPage(w http.ResponseWriter, r *http.Request, p page) {
	p.Title, p.Nav = "CLI", "cli"
	p.CLIGroups, p.CLIPrompt, p.CLICount = cli.Groups(), cli.Prompt, len(cli.Catalog)
	p.Tokens, p.PanelHost = s.store.ListTokens(), r.Host
	s.render(w, r, "cli", p)
}
