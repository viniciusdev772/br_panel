package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	StatusIdle      = "idle"
	StatusDeploying = "deploying"
	StatusDeployed  = "deployed"
	StatusFailed    = "failed"
)

type App struct {
	Name       string    `json:"name"`
	Repo       string    `json:"repo"`
	Branch     string    `json:"branch"`
	BuildCmd   string    `json:"build_cmd"`
	StartCmd   string    `json:"start_cmd"`
	Port       int       `json:"port"`
	Domain     string    `json:"domain"`
	AutoDeploy bool      `json:"auto_deploy"`
	Env        string    `json:"env"`
	Status     string    `json:"status"`
	LastDeploy time.Time `json:"last_deploy"`
}

func (a App) Deploying() bool { return a.Status == StatusDeploying }

// Store keeps apps in a JSON file, written atomically.
type Store struct {
	mu   sync.Mutex
	path string
	apps map[string]App
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, apps: map[string]App{}}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		var list []App
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, err
		}
		for _, a := range list {
			if a.Status == StatusDeploying { // interrupted by a restart
				a.Status = StatusFailed
			}
			s.apps[a.Name] = a
		}
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	list := make([]App, 0, len(s.apps))
	for _, a := range s.apps {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List() []App {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]App, 0, len(s.apps))
	for _, a := range s.apps {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (s *Store) Get(name string) (App, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[name]
	return a, ok
}

func (s *Store) Put(a App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps[a.Name] = a
	return s.saveLocked()
}

func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apps, name)
	return s.saveLocked()
}

// BeginDeploy marks the app as deploying; false if it is missing or already deploying.
func (s *Store) BeginDeploy(name string) (App, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[name]
	if !ok || a.Status == StatusDeploying {
		return a, false
	}
	a.Status = StatusDeploying
	s.apps[name] = a
	_ = s.saveLocked()
	return a, true
}

func (s *Store) FinishDeploy(name string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, found := s.apps[name]
	if !found {
		return
	}
	a.Status = StatusFailed
	if ok {
		a.Status = StatusDeployed
	}
	a.LastDeploy = time.Now()
	s.apps[name] = a
	_ = s.saveLocked()
}

type Paths struct{ Data, Units string }

func (p Paths) AppsFile() string         { return filepath.Join(p.Data, "apps.json") }
func (p Paths) AppDir(n string) string   { return filepath.Join(p.Data, "apps", n) }
func (p Paths) SrcDir(n string) string   { return filepath.Join(p.AppDir(n), "src") }
func (p Paths) LogFile(n string) string  { return filepath.Join(p.Data, "logs", n+".log") }
func (p Paths) EnvFile(n string) string  { return filepath.Join(p.Data, "env", n+".env") }
func (p Paths) UnitFile(n string) string { return filepath.Join(p.Units, unitName(n)) }
