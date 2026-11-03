package monorepo

import "testing"

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pat, path string
		want      bool
	}{
		{"apps/web/**", "apps/web/src/a.ts", true},
		{"apps/web/**", "apps/web", true},
		{"apps/web/**", "apps/webx/a", false},
		{"apps/web", "apps/web/src/a.ts", true}, // a folder name covers everything under it
		{"apps/web", "apps/web", true},
		{"apps/web", "apps/website/a", false},
		{"packages/*", "packages/ui/a.ts", true},
		{"packages/*/src/**", "packages/ui/src/a/b.ts", true},
		{"packages/*/src/**", "packages/ui/lib/a.ts", false},
		{"**/*.md", "docs/a/b.md", true},
		{"**/*.md", "README.md", true},
		{"**/*.md", "a/b.ts", false},
		{"*.json", "package.json", true},
		{"*.json", "a/package.json", false},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"libs/**/index.ts", "libs/index.ts", true},
		{"libs/**/index.ts", "libs/x/y/index.ts", true},
		{"libs/**/index.ts", "libs/x/y/other.ts", false},
		{"ap*s/w*", "apps/web/x", true},
		{"**", "qualquer/coisa", true},
	} {
		if got := Match(c.pat, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, quero %v", c.pat, c.path, got, c.want)
		}
	}
}

func TestAffected(t *testing.T) {
	for _, c := range []struct {
		name    string
		root    string
		watch   []string
		changed []string
		want    bool
	}{
		{"app do repositório inteiro: qualquer push", "", nil, []string{"README.md"}, true},
		{"dentro da pasta", "apps/web", nil, []string{"apps/web/src/a.ts"}, true},
		{"outra app", "apps/web", nil, []string{"apps/api/src/a.ts"}, false},
		{"prefixo parecido não conta", "apps/web", nil, []string{"apps/website/a.ts"}, false},
		{"lockfile da raiz redeploya todas", "apps/web", nil, []string{"pnpm-lock.yaml"}, true},
		{"turbo.json da raiz", "apps/web", nil, []string{"turbo.json"}, true},
		{"tsconfig.base.json da raiz", "apps/web", nil, []string{"tsconfig.base.json"}, true},
		{"README da raiz não redeploya", "apps/web", nil, []string{"README.md", ".github/workflows/ci.yml"}, false},
		{"pacote irmão listado em watch", "apps/web", []string{"packages/ui/**"}, []string{"packages/ui/button.tsx"}, true},
		{"pacote irmão fora do watch", "apps/web", []string{"packages/ui/**"}, []string{"packages/db/x.ts"}, false},
		{"só watch, sem pasta: casa", "", []string{"services/**"}, []string{"services/a/b.go"}, true},
		{"só watch, sem pasta: não casa", "", []string{"services/**"}, []string{"docs/a.md"}, false},
		{"exclusão no fim vence (docs)", "apps/web", []string{"!**/*.md"}, []string{"apps/web/README.md"}, false},
		{"exclusão não tira o resto", "apps/web", []string{"!**/*.md"}, []string{"apps/web/README.md", "apps/web/a.ts"}, true},
		{"o último que casa vence: volta a incluir", "apps/web", []string{"!apps/web/docs/**", "apps/web/docs/api/**"}, []string{"apps/web/docs/api/x.md"}, true},
		{"o último que casa vence: exclui", "apps/web", []string{"apps/web/docs/api/**", "!apps/web/docs/**"}, []string{"apps/web/docs/api/x.md"}, false},
		{"exclusão também vale para arquivo global", "apps/web", []string{"!package.json"}, []string{"package.json"}, false},
		{"arquivo apagado dentro da pasta", "apps/web", nil, []string{"apps/web"}, true},
		{"nada mudou", "apps/web", nil, nil, false},
	} {
		if got := Affected(c.root, c.watch, c.changed); got != c.want {
			t.Errorf("%s: Affected = %v, quero %v", c.name, got, c.want)
		}
	}
}

func TestChangedPaths(t *testing.T) {
	paths, ok := ChangedPaths([]Commit{
		{Added: []string{"a"}, Modified: []string{"b", "a"}},
		{Removed: []string{"c"}},
	})
	if !ok || len(paths) != 3 || paths[0] != "a" || paths[1] != "b" || paths[2] != "c" {
		t.Errorf("paths = %v ok=%v", paths, ok)
	}
	if _, ok := ChangedPaths(nil); ok {
		t.Error("sem commits (push sem novos commits) não prova nada: deployar tudo")
	}
	if _, ok := ChangedPaths([]Commit{{}, {}}); ok {
		t.Error("commits sem arquivo nenhum não provam nada: deployar tudo")
	}
	many := make([]Commit, maxTrustedCommits)
	if _, ok := ChangedPaths(many); ok {
		t.Error("um push com tantos commits quanto o limite pode ter sido cortado")
	}
}
