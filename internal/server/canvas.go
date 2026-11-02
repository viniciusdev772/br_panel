package server

import (
	"sort"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/dbengine"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

// card is one resource (app or database) on the architecture canvas.
type card struct {
	Type        string // app | db
	Name        string
	Sub         string
	Badge       string
	BadgeClass  string // b-<kind>: brand-like color defined in app.css
	Href        string
	StatusText  string
	StatusClass string // ok | warn | bad | muted
	Time        time.Time
}

// edge is a dashed arrow from an app to a database it references.
type edge struct{ From, To string }

type badge struct{ Text, Class string }

var appBadges = []struct {
	Prefix string
	B      badge
}{
	{"Node", badge{"JS", "b-js"}}, {"Go", badge{"Go", "b-go"}}, {"Python", badge{"Py", "b-py"}},
	{"Ruby", badge{"Rb", "b-rb"}}, {"Rust", badge{"Rs", "b-rs"}}, {"PHP", badge{"PHP", "b-php"}},
	{"Site", badge{"Ht", "b-ht"}},
}

func appBadge(kind string) badge {
	for _, b := range appBadges {
		if strings.HasPrefix(kind, b.Prefix) {
			return b.B
		}
	}
	return badge{"App", "b-app"}
}

// canvas builds the cards and the app→database edges of the dashboard.
func (s *Server) canvas() ([]card, []edge) {
	var cards []card
	apps, dbs := s.store.List(), s.store.ListDatabases()
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	for _, a := range apps {
		b := appBadge(a.Kind)
		c := card{Type: "app", Name: a.Name, Badge: b.Text, BadgeClass: b.Class, Href: "/apps/" + a.Name, Time: a.LastDeploy}
		c.Sub = a.Domain
		if c.Sub == "" {
			c.Sub = a.Kind
		}
		switch a.Status {
		case store.StatusDeploying:
			c.StatusText, c.StatusClass = "Implantando…", "warn"
		case store.StatusFailed:
			c.StatusText, c.StatusClass = "Deploy falhou", "bad"
		case store.StatusDeployed:
			c.StatusText, c.StatusClass = "Deploy concluído", "ok"
			if st := systemd.StateUnits(systemd.ReplicaUnits(a.Name, a.Replicas)); st != "active" && st != "unknown" {
				c.StatusText, c.StatusClass = "Serviço "+st, "warn"
			}
		default:
			c.StatusText, c.StatusClass = "Aguardando deploy", "muted"
		}
		cards = append(cards, c)
	}
	for _, d := range dbs {
		sp, _ := dbengine.SpecOf(dbengine.Kind(d.Kind))
		c := card{Type: "db", Name: d.Name, Badge: sp.Badge, BadgeClass: "b-" + d.Kind, Href: "/databases/" + d.Name, Time: d.CreatedAt,
			Sub: sp.Label + " · porta " + itoa(d.Port)}
		switch d.Status {
		case store.DBProvisioning:
			c.StatusText, c.StatusClass = "Provisionando…", "warn"
		case store.DBFailed:
			c.StatusText, c.StatusClass = "Falhou ao provisionar", "bad"
		default:
			if st := systemd.StateUnit(systemd.DBUnitName(d.Name)); st == "active" {
				c.StatusText, c.StatusClass = "Online", "ok"
			} else {
				c.StatusText, c.StatusClass = "Serviço "+st, "warn"
			}
		}
		cards = append(cards, c)
	}
	isDB := map[string]bool{}
	for _, d := range dbs {
		isDB[d.Name] = true
	}
	var edges []edge
	for _, a := range apps {
		for _, ref := range dbengine.References(a.Env) {
			if isDB[ref] {
				edges = append(edges, edge{From: a.Name, To: ref})
			}
		}
	}
	return cards, edges
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	return string(b[i:])
}
