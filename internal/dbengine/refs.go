package dbengine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/store"
)

// PrivateHost is how apps on the same server reach a database.
const PrivateHost = "127.0.0.1"

var refRe = regexp.MustCompile(`\$\{\{\s*([a-z][a-z0-9-]{1,31})\.([A-Z][A-Z0-9_]*)\s*\}\}`)

// Vars returns the variables a database offers to other services, in the spirit of
// "reference variables": ${{<name>.<VAR>}}. host is the address apps should use.
func Vars(d store.Database, host string) map[string]string {
	inst := Instance{Database: d}
	vars := map[string]string{
		"HOST": host, "PORT": fmt.Sprint(d.Port), "USER": d.Username, "PASSWORD": d.Password, "DATABASE": d.DBName,
	}
	if e, err := engineOf(Kind(d.Kind)); err == nil {
		u := e.url(inst, host)
		vars["URL"] = u
		if spec, ok := SpecOf(Kind(d.Kind)); ok && spec.URLVar != "" {
			vars[spec.URLVar] = u
		}
		if Kind(d.Kind) == MongoDB {
			vars["MONGODB_URI"] = u
		}
	}
	return vars
}

// VarNames lists a database's variable names, sorted (URL first).
func VarNames(d store.Database) []string {
	var names []string
	for k := range Vars(d, PrivateHost) {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "URL") != (names[j] == "URL") {
			return names[i] == "URL"
		}
		return names[i] < names[j]
	})
	return names
}

// Resolve substitutes ${{db.VAR}} references in value. Unknown references are reported and left as is.
func Resolve(value string, dbs []store.Database) (string, []string) {
	byName := map[string]store.Database{}
	for _, d := range dbs {
		byName[d.Name] = d
	}
	var unknown []string
	out := refRe.ReplaceAllStringFunc(value, func(m string) string {
		sub := refRe.FindStringSubmatch(m)
		d, ok := byName[sub[1]]
		if ok {
			if v, ok := Vars(d, PrivateHost)[sub[2]]; ok {
				return v
			}
		}
		unknown = append(unknown, sub[1]+"."+sub[2])
		return m
	})
	return out, unknown
}

// References lists the database names mentioned in text (deduplicated, in order of appearance).
func References(text string) []string {
	var names []string
	seen := map[string]bool{}
	for _, m := range refRe.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	return names
}

// CheckRefs returns an error naming references that do not exist.
func CheckRefs(text string, dbs []store.Database) error {
	_, unknown := Resolve(text, dbs)
	if len(unknown) > 0 {
		return fmt.Errorf("referência de variável inexistente: %s", strings.Join(unknown, ", "))
	}
	return nil
}
