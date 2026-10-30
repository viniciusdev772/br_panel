package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/deploy"
	"github.com/viniciusdev772/br_panel/internal/detect"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

type detectResponse struct {
	*detect.Detection
	Name    string   `json:"name"`
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// handleDetect powers the "Detectar automaticamente" button of the new-app form.
func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.PostFormValue("repo"))
	branch := strings.TrimSpace(r.PostFormValue("branch"))
	if branch == "" {
		branch = "main"
	}
	reply := func(code int, v detectResponse) {
		writeJSONStatus(w, code, v)
	}
	if err := validate.Repo(repo); err != nil {
		reply(http.StatusUnprocessableEntity, detectResponse{Error: err.Error()})
		return
	}
	if err := validate.Branch(branch); err != nil {
		reply(http.StatusUnprocessableEntity, detectResponse{Error: err.Error()})
		return
	}
	det, missing, err := s.dep.DetectRepo(r.Context(), repo, branch)
	var cloneErr deploy.ErrCloneFailed
	switch {
	case errors.As(err, &cloneErr):
		reply(http.StatusBadGateway, detectResponse{Error: cloneErr.Error()})
	case err != nil:
		reply(http.StatusInternalServerError, detectResponse{Error: err.Error()})
	case det == nil:
		reply(http.StatusOK, detectResponse{Name: deploy.SuggestName(repo), Error: "não reconheci o tipo do projeto; informe os comandos manualmente"})
	default:
		reply(http.StatusOK, detectResponse{Detection: det, Name: deploy.SuggestName(repo), Missing: missing})
	}
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
