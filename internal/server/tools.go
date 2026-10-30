package server

import (
	"net/http"

	"github.com/viniciusdev772/br_panel/internal/nodeenv"
)

// handleNodeInstall starts the nvm install of Node.js in the background.
func (s *Server) handleNodeInstall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"started": s.node.Start()})
}

// handleNodeStatus reports the install progress and whether node and npm are now on the PATH.
func (s *Server) handleNodeStatus(w http.ResponseWriter, r *http.Request) {
	running, errMsg, log := s.node.State()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"running": running, "error": errMsg, "log": log, "available": nodeenv.Available()})
}
