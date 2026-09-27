package actionlint

import (
	"io"
	"strings"
	"testing"
)

func TestInlineIgnoreOnlySuppressesAdjacentDiagnostic(t *testing.T) {
	source := []byte(`on:
  push:
    # yactionlint ignore=unexpected key "branch"
    branch: main
    other: main
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`)
	linter, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	errs, err := linter.Lint("workflow.yml", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].Kind != "syntax-check" || errs[0].Line != 5 {
		t.Fatalf("expected only the non-adjacent invalid key at line 5: %v", errs)
	}
}

func TestInlineIgnoreRejectsInvalidRegex(t *testing.T) {
	source := []byte("on:\n  push:\n    # yactionlint ignore=[\n    branch: main\n")
	linter, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	errs, err := linter.Lint("workflow.yml", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range errs {
		found = found || strings.Contains(finding.Message, "invalid inline ignore pattern")
	}
	if !found {
		t.Fatalf("invalid regex must not silently suppress diagnostics: %v", errs)
	}
}

func TestInlineIgnoreDoesNotReadScriptComments(t *testing.T) {
	source := []byte(`on: push
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: |
          # yactionlint ignore=unexpected key
          echo ok
        with:
          bad: true
`)
	linter, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	errs, err := linter.Lint("workflow.yml", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].Kind != "syntax-check" || errs[0].Line != 9 {
		t.Fatalf("script comments must not suppress workflow diagnostics: %v", errs)
	}
}

func TestInlineIgnoreRequiresPattern(t *testing.T) {
	source := []byte("on:\n  push:\n    # yactionlint ignore=\n    branch: main\n")
	linter, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	errs, err := linter.Lint("workflow.yml", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range errs {
		found = found || strings.Contains(finding.Message, "inline ignore pattern must not be empty")
	}
	if !found {
		t.Fatalf("empty pattern must not suppress errors: %v", errs)
	}
}
