package actionlint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredNode20ActionMetadata(t *testing.T) {
	for _, tc := range []struct {
		using string
		want  int
	}{{"node20", 1}, {"node24", 0}} {
		t.Run(tc.using, func(t *testing.T) {
			source := []byte("name: Demo\ndescription: JavaScript action\nruns:\n  using: " + tc.using + "\n  main: index.js\n")
			_, action, _, errs := ParseFile("action.yml", source, FileAction)
			if len(errs) != 0 || action == nil {
				t.Fatalf("valid metadata rejected: %v", errs)
			}
			rule := NewRuleActionMetadata()
			if err := rule.VisitActionPre(action); err != nil {
				t.Fatal(err)
			}
			if got := rule.Errs(); len(got) != tc.want {
				t.Fatalf("node runner retirement diagnostics: %v", got)
			}
		})
	}
}

func TestRetiredNode20LocalActionUse(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "actions", "demo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("console.log('ok')\n"), 0644); err != nil {
		t.Fatal(err)
	}
	workflow := testParseWorkflow(t, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/demo\n")
	for _, tc := range []struct {
		using string
		want  int
	}{{"node20", 1}, {"node24", 0}} {
		t.Run(tc.using, func(t *testing.T) {
			metadata := "name: Demo\ndescription: JavaScript action\nruns:\n  using: " + tc.using + "\n  main: index.js\n"
			if err := os.WriteFile(filepath.Join(dir, "action.yml"), []byte(metadata), 0644); err != nil {
				t.Fatal(err)
			}
			rule := NewRuleAction(NewLocalActionsCache(&Project{root: root}, nil))
			if got := testVisitActionRule(t, rule, workflow); len(got) != tc.want {
				t.Fatalf("node runner retirement diagnostics: %v", got)
			}
		})
	}
}
