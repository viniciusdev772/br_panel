package store

import (
	"encoding/json"
	"slices"
)

// Web holds the options of an app that live in its Nginx vhost. They have their own tab and their
// own save, apart from the app form, so saving that form never touches them.
type Web struct {
	Domains     []WebDomain `json:"domains,omitempty"`
	Users       []WebUser   `json:"users,omitempty"`
	Maintenance bool        `json:"maint,omitempty"`
	MaintTitle  string      `json:"maint_title,omitempty"`
	MaintText   string      `json:"maint_text,omitempty"`
	MaxBodyMB   int         `json:"max_body_mb,omitempty"`  // 0 = the Nginx default
	ReadTimeout int         `json:"read_timeout,omitempty"` // seconds; 0 = the default of 300
}

// WebDomain is one more domain of the app: it serves the app, or, with To set, redirects.
type WebDomain struct {
	Name      string `json:"name"`
	To        string `json:"to,omitempty"`
	Permanent bool   `json:"perm,omitempty"`
}

// WebUser is a basic-auth user. Hash is a bcrypt hash and is never shown on any page.
type WebUser struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// HasDomain reports whether name is one of the extra domains.
func (w Web) HasDomain(name string) bool {
	return slices.ContainsFunc(w.Domains, func(d WebDomain) bool { return d.Name == name })
}

// HasUser reports whether name is one of the basic-auth users.
func (w Web) HasUser(name string) bool {
	return slices.ContainsFunc(w.Users, func(u WebUser) bool { return u.Name == name })
}

func (w Web) json() string {
	if len(w.Domains) == 0 && len(w.Users) == 0 && !w.Maintenance && w.MaintTitle == "" && w.MaintText == "" && w.MaxBodyMB == 0 && w.ReadTimeout == 0 {
		return ""
	}
	b, _ := json.Marshal(w)
	return string(b)
}

// SetWeb saves the Nginx options of an app.
func (s *Store) SetWeb(app string, w Web) error {
	_, err := s.db.Exec(`UPDATE apps SET web = ? WHERE name = ?`, w.json(), app)
	return err
}
