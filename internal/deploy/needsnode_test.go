package deploy

import "testing"

func TestNeedsNode(t *testing.T) {
	for _, tc := range []struct {
		requires []string
		want     bool
	}{
		{[]string{"node", "npm"}, true},
		{[]string{"pnpm"}, true},
		{[]string{"yarn", "node"}, true},
		{[]string{"python3"}, false},
		{[]string{"go"}, false},
		{nil, false},
	} {
		if got := needsNode(tc.requires); got != tc.want {
			t.Errorf("needsNode(%v) = %v", tc.requires, got)
		}
	}
}
