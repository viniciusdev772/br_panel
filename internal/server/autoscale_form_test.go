package server

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postedApp(t *testing.T, extra url.Values) (a struct {
	enabled                     bool
	min, max, cpu, mem, out, in int
	replicas                    int
}, err error) {
	t.Helper()
	v := url.Values{"repo": {"https://github.com/o/r.git"}, "branch": {"main"}}
	for k, vals := range extra {
		v[k] = vals
	}
	req := httptest.NewRequest("POST", "/apps", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app, err := appFromForm(req, "web")
	a.enabled, a.min, a.max, a.cpu, a.mem = app.AutoScale.Enabled, app.AutoScale.Min, app.AutoScale.Max, app.AutoScale.TargetCPU, app.AutoScale.TargetMem
	a.out, a.in, a.replicas = app.AutoScale.OutCooldown, app.AutoScale.InCooldown, app.Replicas
	return a, err
}

func TestAppFromFormAutoScale(t *testing.T) {
	// enabled with empty fields: the defaults shown in the form
	a, err := postedApp(t, url.Values{"autoscale": {"on"}})
	if err != nil || !a.enabled || a.min != 1 || a.max != 4 || a.cpu != 60 || a.replicas != 1 {
		t.Fatalf("%+v %v", a, err)
	}
	// the instance count is pulled into the range
	a, err = postedApp(t, url.Values{"autoscale": {"on"}, "scale_min": {"3"}, "scale_max": {"6"}, "scale_cpu": {"70"}, "replicas": {"1"}})
	if err != nil || a.replicas != 3 || a.cpu != 70 {
		t.Fatalf("abaixo do mínimo: %+v %v", a, err)
	}
	a, err = postedApp(t, url.Values{"autoscale": {"on"}, "scale_min": {"1"}, "scale_max": {"2"}, "replicas": {"9"}})
	if err != nil || a.replicas != 2 {
		t.Fatalf("acima do máximo: %+v %v", a, err)
	}
	// off: the typed values are kept but nothing is enforced
	a, err = postedApp(t, url.Values{"scale_min": {"2"}, "scale_max": {"5"}, "scale_cpu": {"50"}, "replicas": {"1"}})
	if err != nil || a.enabled || a.replicas != 1 || a.max != 5 {
		t.Fatalf("desligado: %+v %v", a, err)
	}

	for name, extra := range map[string]url.Values{
		"mínimo maior que o máximo": {"autoscale": {"on"}, "scale_min": {"5"}, "scale_max": {"2"}},
		"máximo acima de 16":        {"autoscale": {"on"}, "scale_max": {"17"}},
		"meta de CPU fora da faixa": {"autoscale": {"on"}, "scale_cpu": {"99"}},
		"meta de CPU muito baixa":   {"autoscale": {"on"}, "scale_cpu": {"5"}},
		"memória sem limite":        {"autoscale": {"on"}, "scale_mem": {"70"}},
		"espera negativa":           {"autoscale": {"on"}, "scale_out_cd": {"-1"}},
		"espera de mais de 1 hora":  {"autoscale": {"on"}, "scale_in_cd": {"3601"}},
	} {
		if _, err := postedApp(t, extra); err == nil || !strings.Contains(err.Error(), "escalonamento") {
			t.Errorf("%s: deveria ser rejeitado, err = %v", name, err)
		}
	}
	// memory target is fine once the memory limit exists
	if _, err := postedApp(t, url.Values{"autoscale": {"on"}, "scale_mem": {"70"}, "memory_mb": {"512"}}); err != nil {
		t.Errorf("meta de memória com limite: %v", err)
	}
}
