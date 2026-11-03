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
	Name    string             `json:"name"`
	Root    string             `json:"root,omitempty"` // folder the detection ran in (monorepo)
	Apps    []detect.Candidate `json:"apps,omitempty"` // apps found when the repository is a monorepo
	Missing []string           `json:"missing,omitempty"`
	// Monorepo is true for a root project that also holds other projects (the list is in Apps).
	Monorepo bool            `json:"monorepo,omitempty"`
	Env      []detect.EnvVar `json:"env_vars,omitempty"` // variables the code reads: set them after creating the app
	Error    string          `json:"error,omitempty"`
}

// handleDetect powers the "Detectar automaticamente" button of the new-app form.
func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.PostFormValue("repo"))
	branch := strings.TrimSpace(r.PostFormValue("branch"))
	if branch == "" {
		branch = "main"
	}
	root, rootErr := validate.RootDir(r.PostFormValue("root_dir"))
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
	if rootErr != nil {
		reply(http.StatusUnprocessableEntity, detectResponse{Error: rootErr.Error()})
		return
	}
	res, err := s.dep.DetectRepo(r.Context(), repo, branch, root)
	name := deploy.SuggestNameIn(repo, root)
	var cloneErr deploy.ErrCloneFailed
	switch {
	case errors.As(err, &cloneErr):
		reply(http.StatusBadGateway, detectResponse{Error: cloneErr.Error()})
	case err != nil && root != "": // the folder is not in the repository, or is not a plain folder
		reply(http.StatusUnprocessableEntity, detectResponse{Error: err.Error()})
	case err != nil:
		reply(http.StatusInternalServerError, detectResponse{Error: err.Error()})
	case res.Det == nil && len(res.Apps) > 0 && root == "":
		reply(http.StatusOK, detectResponse{Name: name, Apps: res.Apps, Error: "este repositório é um monorepo: escolha a aplicação que quer publicar"})
	case res.Det == nil:
		reply(http.StatusOK, detectResponse{Name: name, Root: root, Apps: res.Apps, Error: "não reconheci o tipo do projeto; informe os comandos manualmente"})
	default:
		reply(http.StatusOK, detectResponse{Detection: res.Det, Name: name, Root: root, Apps: res.Apps, Missing: res.Missing, Monorepo: res.Monorepo, Env: res.Env})
	}
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
