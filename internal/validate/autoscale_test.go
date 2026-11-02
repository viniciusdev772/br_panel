package validate

import "testing"

func TestValidateAutoScale(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		on                                           bool
		min, max, cpu, mem, outCD, inCD, memoryLimit int
		ok                                           bool
	}{
		{"desligado ignora tudo", false, 9, 1, 0, 0, -5, 0, 0, true},
		{"válido", true, 1, 4, 60, 0, 0, 0, 0, true},
		{"mínimo igual ao máximo", true, 3, 3, 60, 0, 0, 0, 0, true},
		{"com memória e limite", true, 1, 4, 60, 70, 60, 300, 512, true},
		{"mínimo zero", true, 0, 4, 60, 0, 0, 0, 0, false},
		{"máximo menor que o mínimo", true, 5, 2, 60, 0, 0, 0, 0, false},
		{"máximo acima de 16", true, 1, 17, 60, 0, 0, 0, 0, false},
		{"CPU 9%", true, 1, 4, 9, 0, 0, 0, 0, false},
		{"CPU 96%", true, 1, 4, 96, 0, 0, 0, 0, false},
		{"memória 99% com limite", true, 1, 4, 60, 99, 0, 0, 512, false},
		{"memória sem limite", true, 1, 4, 60, 70, 0, 0, 0, false},
		{"espera negativa", true, 1, 4, 60, 0, -1, 0, 0, false},
		{"espera de 3601 s", true, 1, 4, 60, 0, 0, 3601, 0, false},
	} {
		if err := AutoScale(tc.on, tc.min, tc.max, tc.cpu, tc.mem, tc.outCD, tc.inCD, tc.memoryLimit); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
