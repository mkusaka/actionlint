package actionlint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testParseWorkflow(t *testing.T, src string) *Workflow {
	t.Helper()
	w, errs := Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if w == nil {
		t.Fatal("workflow was not parsed")
	}
	return w
}

func testVisitActionRule(t *testing.T, rule *RuleAction, w *Workflow) []*Error {
	t.Helper()
	v := NewVisitor()
	v.AddPass(rule)
	if err := v.Visit(w); err != nil {
		t.Fatal(err)
	}
	return rule.Errs()
}

func TestRuleActionRequireCommitHash(t *testing.T) {
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@master
      - uses: actions/setup-node@0123456789abcdef0123456789abcdef01234567
      - uses: actions/cache@0123456789ABCDEF0123456789ABCDEF01234567
      - uses: docker://alpine:latest
      - uses: ${{ github.repository }}/action@main
`

	t.Run("disabled by default", func(t *testing.T) {
		rule := NewRuleAction(newNullLocalActionsCache(nil))
		if errs := testVisitActionRule(t, rule, testParseWorkflow(t, workflow)); len(errs) != 0 {
			t.Fatalf("got unexpected errors: %v", errs)
		}
	})

	t.Run("requires full commit hash", func(t *testing.T) {
		rule := NewRuleAction(newNullLocalActionsCache(nil))
		rule.SetConfig(&Config{RequireCommitHash: true})
		errs := testVisitActionRule(t, rule, testParseWorkflow(t, workflow))
		if len(errs) != 1 {
			t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
		}
		if got, want := errs[0].Message, `action "actions/checkout@master" must be pinned to a full-length commit SHA`; got != want {
			t.Fatalf("got error %q, want %q", got, want)
		}
	})
}

func TestRuleActionRequireCommitHashInLocalCompositeAction(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "actions", "composite")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	metadata := `name: composite
description: test composite
runs:
  using: composite
  steps:
    - uses: actions/cache@v4
    - uses: actions/setup-node@0123456789abcdef0123456789abcdef01234567
    - uses: docker://alpine:latest
    - uses: ./.github/actions/other
`
	if err := os.WriteFile(filepath.Join(dir, "action.yml"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}
	workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/composite
`

	t.Run("disabled by default", func(t *testing.T) {
		rule := NewRuleAction(NewLocalActionsCache(&Project{root: root}, nil))
		if errs := testVisitActionRule(t, rule, testParseWorkflow(t, workflow)); len(errs) != 0 {
			t.Fatalf("got unexpected errors: %v", errs)
		}
	})

	t.Run("checks nested repository actions", func(t *testing.T) {
		rule := NewRuleAction(NewLocalActionsCache(&Project{root: root}, nil))
		rule.SetConfig(&Config{RequireCommitHash: true})
		errs := testVisitActionRule(t, rule, testParseWorkflow(t, workflow))
		if len(errs) != 1 {
			t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Message, `action "actions/cache@v4" in local composite action`) {
			t.Fatalf("unexpected error: %q", errs[0].Message)
		}
	})
}
