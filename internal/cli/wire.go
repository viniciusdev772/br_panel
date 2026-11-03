package cli

// AppInfo is the JSON the panel serves at /api/apps/{name}. The server fills it, the CLI reads it.
type AppInfo struct {
	Name       string `json:"name"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	BuildCmd   string `json:"build_cmd"`
	StartCmd   string `json:"start_cmd"`
	Port       int    `json:"port"`
	Domain     string `json:"domain"`
	URL        string `json:"url,omitempty"`
	AutoDeploy bool   `json:"auto_deploy"`
	Env        string `json:"env,omitempty"`
	Replicas   int    `json:"replicas"`
	MemoryMB   int    `json:"memory_mb"`
	CPUPercent int    `json:"cpu_percent"`
	RateLimit  int    `json:"rate_limit"`
	Kind       string `json:"kind"`
	Status     string `json:"status"` // idle | deploying | deployed | failed
	State      string `json:"state"`  // systemd state: active, inactive, failed…
	Deploying  bool   `json:"deploying"`
	LastDeploy string `json:"last_deploy,omitempty"`

	Domains     []DomainInfo `json:"extra_domains"`
	AuthUsers   []string     `json:"auth_users"`
	Maintenance bool         `json:"maintenance"`
	MaintTitle  string       `json:"maint_title,omitempty"`
	MaintText   string       `json:"maint_text,omitempty"`
	UploadMB    int          `json:"upload_mb"`
	ReadTimeout int          `json:"read_timeout"`

	AutoScale ScaleInfo `json:"autoscale"`
}

// DomainInfo is an extra domain of an app: served as an alias or redirected.
type DomainInfo struct {
	Name      string `json:"name"`
	To        string `json:"redirect_to,omitempty"`
	Permanent bool   `json:"permanent,omitempty"`
}

// ScaleInfo is the autoscale configuration of an app.
type ScaleInfo struct {
	Enabled bool `json:"enabled"`
	Min     int  `json:"min"`
	Max     int  `json:"max"`
	CPU     int  `json:"cpu"`
	Mem     int  `json:"mem"`
	OutCD   int  `json:"out_cd"`
	InCD    int  `json:"in_cd"`
}

// DBInfo is the JSON the panel serves at /api/databases. Passwords are never included: apps connect
// through the ${{nome.VARIAVEL}} references in Refs.
type DBInfo struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Port     int      `json:"port"`
	Status   string   `json:"status"` // provisioning | ready | failed
	State    string   `json:"state"`
	Public   bool     `json:"public"`
	Schedule string   `json:"backup_schedule"`
	Created  string   `json:"created_at"`
	UsedBy   []string `json:"used_by"`
	Refs     []string `json:"refs"`
}

// TemplateInfo is the JSON the panel serves at /api/templates: one installable app.
type TemplateInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	VersionHint string   `json:"version_hint"`
	Needs       []string `json:"needs"`
	Docs        string   `json:"docs"`
	After       string   `json:"after"`
	Source      string   `json:"source"`
	MemoryMB    int      `json:"memory_mb"`
}
