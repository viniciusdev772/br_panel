package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/ai"
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
	AI       bool            `json:"ai,omitempty"` // the commands came from the AI assisted detection
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
	useAI := r.PostFormValue("ai") == "1"
	var provider ai.Provider
	if useAI {
		set := s.settings()
		p, err := ai.New(set.AIFormat, set.AIBaseURL, set.AIKey, set.AIModel)
		if err != nil {
			reply(http.StatusUnprocessableEntity, detectResponse{Error: "Configure a IA em Configurações (modelo e chave) para usar a descoberta com IA."})
			return
		}
		provider = p
	}
	var res deploy.RepoDetection
	var err error
	if useAI {
		res, err = s.dep.DetectRepoSnapshot(r.Context(), repo, branch, root)
	} else {
		res, err = s.dep.DetectRepo(r.Context(), repo, branch, root)
	}
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
	case useAI:
		s.replyDetectAI(r.Context(), reply, provider, res, name, root)
	case res.Det == nil:
		reply(http.StatusOK, detectResponse{Name: name, Root: root, Apps: res.Apps, Error: "não reconheci o tipo do projeto; informe os comandos manualmente"})
	default:
		reply(http.StatusOK, detectResponse{Detection: res.Det, Name: name, Root: root, Apps: res.Apps, Missing: res.Missing, Monorepo: res.Monorepo, Env: res.Env})
	}
}

// replyDetectAI answers the AI assisted detection: the model reads a snapshot of the project (file
// tree and key files, never secrets) and suggests the commands, which only fill the form. A project
// the static detector already knows keeps its own answer as the base, so the model adds to it.
func (s *Server) replyDetectAI(ctx context.Context, reply func(int, detectResponse), p ai.Provider, res deploy.RepoDetection, name, root string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	d, err := ai.Detect(ctx, p, res.Snapshot)
	if err != nil {
		reply(http.StatusBadGateway, detectResponse{Name: name, Root: root, Apps: res.Apps, Error: "A IA não conseguiu analisar: " + err.Error()})
		return
	}
	det := &detect.Detection{Kind: cmp.Or(d.Kind, "projeto"), Build: d.Build, Start: d.Start, Requires: d.Needs, Notes: append([]string{"Sugestão gerada por IA: revise os comandos antes de criar."}, d.Notes...)}
	if det.Start == "" && det.Build == "" {
		reply(http.StatusOK, detectResponse{Name: name, Root: root, Apps: res.Apps, Error: "A IA não achou como iniciar este projeto. " + strings.Join(d.Notes, " ")})
		return
	}
	env := res.Env
	seen := map[string]bool{}
	for _, e := range env {
		seen[e.Name] = true
	}
	for _, e := range d.Env {
		if !seen[e.Name] {
			env = append(env, detect.EnvVar{Name: e.Name, Required: e.Required, Source: "IA"})
		}
	}
	reply(http.StatusOK, detectResponse{Detection: det, Name: name, Root: root, Apps: res.Apps, Missing: detect.MissingRequirements(det, exec.LookPath), Monorepo: res.Monorepo, Env: env, AI: true})
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
