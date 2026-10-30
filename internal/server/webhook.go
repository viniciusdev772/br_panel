package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/githubapp"
)

const webhookMaxBody = 5 << 20

// handleGitHubWebhook receives push events and redeploys matching apps.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.settings().GitHubWebhookSecret
	if secret == "" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "corpo inválido", http.StatusRequestEntityTooLarge)
		return
	}
	if !githubapp.ValidSignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "assinatura inválida", http.StatusUnauthorized)
		return
	}
	switch ev := r.Header.Get("X-GitHub-Event"); ev {
	case "ping":
		_, _ = io.WriteString(w, "pong\n")
	case "push":
		var p struct {
			Ref        string `json:"ref"`
			After      string `json:"after"`
			Deleted    bool   `json:"deleted"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			http.Error(w, "JSON inválido (use Content-Type application/json)", http.StatusBadRequest)
			return
		}
		branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
		if !ok || p.Deleted {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		n := 0
		for _, a := range s.store.List() {
			slug, isGH := githubapp.Slug(a.Repo)
			if !a.AutoDeploy || !isGH || slug != strings.ToLower(p.Repository.FullName) || a.Branch != branch {
				continue
			}
			n++
			reason := fmt.Sprintf("webhook push %s", shortSHA(p.After))
			if !s.dep.Start(a.Name, reason) {
				s.dep.MarkPending(a.Name)
			}
			log.Printf("webhook: deploy de %s (%s)", a.Name, reason)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "%d deploy(s) disparado(s)\n", n)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
