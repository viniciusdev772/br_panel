package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The skills in /skills teach AI agents to drive this CLI. They must never teach a command that does
// not exist, so every command they show goes through the same resolver and parser as a real run.

const skillsDir = "../../skills"

var (
	inlineCode = regexp.MustCompile("`([^`\n]+)`")
	mdLink     = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	skillRef   = regexp.MustCompile("`(brpanel-[a-z]+)`")
	skillName  = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

// mdFiles lists every Markdown file under the skills folder.
func mdFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(skillsDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatalf("não consegui ler %s: %v", skillsDir, err)
	}
	if len(out) == 0 {
		t.Fatalf("nenhum arquivo .md em %s", skillsDir)
	}
	return out
}

// splitMarkdown returns the commands written in ```sh blocks (one per line) and the text left
// after removing every fenced block, where inline `code` is looked for.
func splitMarkdown(src string) (blocks []string, prose string) {
	var keep []string
	inShell, inOther := false, false
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "```"):
			if inShell || inOther {
				inShell, inOther = false, false
			} else if strings.TrimPrefix(trim, "```") == "sh" {
				inShell = true
			} else {
				inOther = true
			}
		case inShell:
			if trim != "" && !strings.HasPrefix(trim, "#") {
				blocks = append(blocks, trim)
			}
		case inOther:
		default:
			keep = append(keep, line)
		}
	}
	return blocks, strings.Join(keep, "\n")
}

// brpanelLine returns the words of the brpanel command in a shell line, or nil when the line runs
// something else. It understands `sudo brpanel ...` and `printf ... | brpanel ...`.
func brpanelLine(line string) []string {
	line = strings.ReplaceAll(line, `\|`, "|") // an escaped pipe inside a Markdown table cell
	if _, after, ok := strings.Cut(line, "| "); ok {
		line = after
	}
	line = strings.TrimPrefix(strings.TrimSpace(line), "sudo ")
	if i := strings.Index(line, " # "); i >= 0 { // trailing comment
		line = line[:i]
	}
	w := words(line)
	if len(w) == 0 || w[0] != "brpanel" {
		return nil
	}
	return w
}

// checkCommand fails the test when a line is not accepted by the CLI. full is true for a line in a
// ```sh block, which must be runnable as written; a command named in a sentence ("`brpanel apps env`
// mostra segredos") only has to exist.
func checkCommand(t *testing.T, where, line string, w []string, full bool) {
	t.Helper()
	if len(w) == 1 && !full { // "brpanel" alone is the program's name in a sentence
		return
	}
	cmd, rest := resolve(w[1:])
	if cmd == nil {
		t.Errorf("%s: %q não é um comando da CLI", where, line)
		return
	}
	pos, _, err := parse(cmd, rest)
	if err != nil {
		t.Errorf("%s: %q não passa no parser: %v", where, line, err)
		return
	}
	if cmd.Args == "" && len(pos) > 0 { // "apps frobnicate loja" would quietly run "apps list"
		t.Errorf("%s: %q não recebe argumentos, mas veio: %s", where, line, strings.Join(pos, " "))
	}
	if full && len(pos) < cmd.Min && cmd.Name != "apps create" {
		t.Errorf("%s: %q tem poucos argumentos (%d < %d)", where, line, len(pos), cmd.Min)
	}
	if full && cmd.Danger && !slicesContains(w, "--yes") {
		t.Errorf("%s: %q apaga dados e precisa de --yes no exemplo", where, line)
	}
}

func slicesContains(w []string, s string) bool {
	for _, x := range w {
		if x == s {
			return true
		}
	}
	return false
}

// A line in prose is checked only when it is a plain, complete command: placeholders such as ...
// or alternatives with | are descriptions, not something to run.
func plainCommand(s string) bool {
	return !strings.ContainsAny(s, "[]<>…") && !strings.Contains(s, "...")
}

func TestSkillsCommandsExist(t *testing.T) {
	checked := 0
	for _, f := range mdFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		blocks, prose := splitMarkdown(string(src))
		for _, line := range blocks {
			if w := brpanelLine(line); w != nil {
				checkCommand(t, f, line, w, true)
				checked++
			}
		}
		for _, m := range inlineCode.FindAllStringSubmatch(prose, -1) {
			span := m[1]
			if !plainCommand(span) {
				continue
			}
			if w := brpanelLine(span); w != nil {
				checkCommand(t, f, span, w, false)
				checked++
			}
		}
	}
	if checked < 100 {
		t.Errorf("só %d comandos conferidos nos skills; o extrator está quebrado?", checked)
	}
}

// Every skill folder has a SKILL.md whose header names the folder and says when to use it.
func TestSkillsHeaders(t *testing.T) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		names = append(names, e.Name())
		b, err := os.ReadFile(filepath.Join(skillsDir, e.Name(), "SKILL.md"))
		if err != nil {
			t.Errorf("%s: falta o SKILL.md", e.Name())
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		if len(lines) < 4 || lines[0] != "---" {
			t.Errorf("%s: o SKILL.md precisa começar com o cabeçalho ---", e.Name())
			continue
		}
		head := map[string]string{}
		closed := false
		for _, l := range lines[1:] {
			if l == "---" {
				closed = true
				break
			}
			k, v, _ := strings.Cut(l, ": ")
			head[k] = strings.TrimSpace(v)
		}
		switch {
		case !closed:
			t.Errorf("%s: o cabeçalho não fecha com ---", e.Name())
		case head["name"] != e.Name() || !skillName.MatchString(head["name"]):
			t.Errorf("%s: name %q precisa ser igual ao nome da pasta", e.Name(), head["name"])
		case len(head["description"]) < 40 || len(head["description"]) > 1024:
			t.Errorf("%s: a description precisa ter de 40 a 1024 caracteres (tem %d)", e.Name(), len(head["description"]))
		}
	}
	if len(names) == 0 {
		t.Fatal("nenhum skill encontrado")
	}
}

// Links point to files that exist, and a skill mentioned by name is a real folder.
func TestSkillsLinks(t *testing.T) {
	for _, f := range mdFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(src), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if _, err := os.Stat(filepath.Join(filepath.Dir(f), target)); err != nil {
				t.Errorf("%s: o link %q não existe", f, m[1])
			}
		}
		for _, m := range skillRef.FindAllStringSubmatch(string(src), -1) {
			if _, err := os.Stat(filepath.Join(skillsDir, m[1], "SKILL.md")); err != nil {
				t.Errorf("%s: o skill %q não existe", f, m[1])
			}
		}
	}
}

// The command reference lists every command of the catalog with each of its options, so adding a
// command or flag without telling the agents fails the build.
func TestSkillsReferenceCoversCatalog(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(skillsDir, "brpanel-cli", "references", "comandos.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	rows := strings.Split(doc, "\n")
	shared := map[string]bool{} // options of apps create/set are described once, in a paragraph
	for _, f := range appFlags {
		shared[f.Name] = true
	}
	for _, f := range globalFlags {
		if !strings.Contains(doc, "`--"+f.Name) {
			t.Errorf("comandos.md não descreve a opção global --%s", f.Name)
		}
	}
	for _, c := range Catalog {
		row := ""
		for _, r := range rows {
			if rest, ok := strings.CutPrefix(r, "| `"+c.Name); ok && (strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "`")) {
				row = r
				break
			}
		}
		if row == "" {
			t.Errorf("comandos.md não tem uma linha para %q", c.Name)
			continue
		}
		for _, f := range c.Flags {
			where := row
			if shared[f.Name] {
				where = doc
			}
			if !strings.Contains(where, "`--"+f.Name) {
				t.Errorf("comandos.md: %q não menciona --%s", c.Name, f.Name)
			}
		}
		if c.Danger && !strings.Contains(row, "--yes") {
			t.Errorf("comandos.md: %q apaga dados e a linha precisa citar --yes", c.Name)
		}
	}
}

// The helper script is plain POSIX sh with Unix line endings (agents run it as-is).
func TestSkillsScript(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(skillsDir, "brpanel-cli", "scripts", "verificar.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "#!/bin/sh\n") || strings.Contains(string(b), "\r") {
		t.Error("verificar.sh precisa começar com #!/bin/sh e usar fim de linha Unix")
	}
}
