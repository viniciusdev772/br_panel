package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/buildcache"
)

const (
	defaultPruneDays = 30
	maxPruneDays     = 3650
)

// cacheInfo is the JSON of /api/cache (also read by `brpanel cache status` and `brpanel status`).
type cacheInfo struct {
	Dir   string            `json:"dir"`
	Total int64             `json:"total"`
	Items []buildcache.Item `json:"items"`
}

func (s *Server) cacheInfo() cacheInfo {
	return cacheInfo{Dir: s.dep.Cache.Dir, Total: s.dep.Cache.Total(), Items: s.dep.Cache.Items()}
}

func (s *Server) handleAPICache(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.cacheInfo()) }

// cacheDone sends the browser (or the CLI, which reads ?msg=) back to the cache card.
func cacheDone(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/settings?msg="+url.QueryEscape(msg)+"#cache", http.StatusSeeOther)
}

// handleCachePrune deletes the cached packages nobody used for the given number of days.
func (s *Server) handleCachePrune(w http.ResponseWriter, r *http.Request) {
	days := defaultPruneDays
	if v := strings.TrimSpace(r.PostFormValue("days")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > maxPruneDays {
			cacheDone(w, r, "Falha: informe os dias sem uso entre 0 e "+strconv.Itoa(maxPruneDays))
			return
		}
		days = n
	}
	if s.dep.Busy() {
		cacheDone(w, r, "Falha: há um deploy em andamento; espere ele terminar para limpar o cache.")
		return
	}
	files, freed, err := s.dep.Cache.Prune(time.Duration(days) * 24 * time.Hour)
	if err != nil {
		cacheDone(w, r, "Falha: "+err.Error())
		return
	}
	s.store.AddEvent("cache", "cache", "Cache limpo: "+strconv.Itoa(files)+" arquivos sem uso há "+strconv.Itoa(days)+" dias, "+humanBytes(freed)+" liberados")
	cacheDone(w, r, "Cache limpo: "+strconv.Itoa(files)+" arquivos sem uso há "+strconv.Itoa(days)+" dias removidos ("+humanBytes(freed)+" liberados).")
}

// handleCacheClear empties the shared cache; the next builds download everything again.
func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if s.dep.Busy() {
		cacheDone(w, r, "Falha: há um deploy em andamento; espere ele terminar para limpar o cache.")
		return
	}
	freed, err := s.dep.Cache.Clear()
	if err != nil {
		cacheDone(w, r, "Falha: "+err.Error())
		return
	}
	s.store.AddEvent("cache", "cache", "Cache esvaziado: "+humanBytes(freed)+" liberados")
	cacheDone(w, r, "Cache esvaziado ("+humanBytes(freed)+" liberados). O próximo deploy baixa os pacotes de novo.")
}
