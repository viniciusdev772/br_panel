package validate

import (
	"strings"
	"testing"
)

func TestExtraDomain(t *testing.T) {
	tests := []struct {
		name, to string
		ok       bool
	}{
		{"www.example.com", "", true},
		{"www.example.com", "example.com", true},
		{"", "", false},
		{"example.com", "", false}, // the main domain
		{"www.example.com", "www.example.com", false},
		{"a b.example.com", "", false},
		{"x.example.com;", "", false},
		{"x.example.com", "y.example.com;return 200", false},
		{"x.example.com", "y.example.com/x", false},
		{"x.example.com", "http://y.example.com", false},
		{"x.example.com\nserver_name y", "", false},
		{"$host", "", false},
	}
	for _, tc := range tests {
		if err := ExtraDomain(tc.name, tc.to, "example.com"); (err == nil) != tc.ok {
			t.Errorf("ExtraDomain(%q, %q) = %v, ok esperado = %v", tc.name, tc.to, err, tc.ok)
		}
	}
}

func TestWebUserAndPassword(t *testing.T) {
	for _, u := range []string{"ana", "ana.silva", "a-b_c", strings.Repeat("a", 32)} {
		if err := WebUser(u); err != nil {
			t.Errorf("WebUser(%q) = %v", u, err)
		}
	}
	for _, u := range []string{"", "a:b", "a b", "a\nb:hash", "ana;", strings.Repeat("a", 33), "a/b", "á"} {
		if err := WebUser(u); err == nil {
			t.Errorf("WebUser(%q) deveria falhar", u)
		}
	}
	if WebPassword("1234567") == nil || WebPassword(strings.Repeat("a", 73)) == nil {
		t.Error("senha curta ou longa demais passou")
	}
	if WebPassword("12345678") != nil || WebPassword(strings.Repeat("a", 72)) != nil {
		t.Error("senha dentro dos limites recusada")
	}
}

func TestWebLimits(t *testing.T) {
	for _, n := range []int{0, 1, 50, MaxUploadMB} {
		if UploadMB(n) != nil {
			t.Errorf("UploadMB(%d)", n)
		}
	}
	for _, n := range []int{-1, MaxUploadMB + 1} {
		if UploadMB(n) == nil {
			t.Errorf("UploadMB(%d) deveria falhar", n)
		}
	}
	for _, n := range []int{0, 10, 300, 3600} {
		if ReadTimeout(n) != nil {
			t.Errorf("ReadTimeout(%d)", n)
		}
	}
	for _, n := range []int{-5, 1, 9, 3601} {
		if ReadTimeout(n) == nil {
			t.Errorf("ReadTimeout(%d) deveria falhar", n)
		}
	}
}

func TestMaintenanceText(t *testing.T) {
	if err := MaintenanceText("Já voltamos <b>", "linha 1\nlinha 2 & \"aspas\""); err != nil {
		t.Errorf("texto comum: %v", err)
	}
	for _, c := range [][2]string{
		{strings.Repeat("é", MaxTitleLen+1), ""},
		{"", strings.Repeat("x", MaxMessageLen+1)},
		{"a\x00b", ""},
	} {
		if MaintenanceText(c[0], c[1]) == nil {
			t.Errorf("MaintenanceText(%.20q, %.20q) deveria falhar", c[0], c[1])
		}
	}
}
