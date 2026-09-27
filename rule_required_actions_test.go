package actionlint

import "testing"

func testVisitRequiredActionsRule(t *testing.T, cfg *Config, src string) []*Error {
	t.Helper()
	rule := NewRuleRequiredActions()
	rule.SetConfig(cfg)
	v := NewVisitor()
	v.AddPass(rule)
	if err := v.Visit(testParseWorkflow(t, src)); err != nil {
		t.Fatal(err)
	}
	return rule.Errs()
}

func TestRuleRequiredActions(t *testing.T) {
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
`

	tests := []struct {
		name     string
		required []RequiredActionRule
		want     []string
	}{
		{
			name: "disabled by default",
		},
		{
			name:     "required action without version is present",
			required: []RequiredActionRule{{Action: "actions/checkout"}},
		},
		{
			name:     "required action version matches any use",
			required: []RequiredActionRule{{Action: "actions/checkout", Version: "v4"}},
		},
		{
			name:     "missing action",
			required: []RequiredActionRule{{Action: "github/codeql-action/upload-sarif"}},
			want:     []string{`required action "github/codeql-action/upload-sarif" is not used in this workflow`},
		},
		{
			name:     "version mismatch",
			required: []RequiredActionRule{{Action: "actions/checkout", Version: "v5"}},
			want:     []string{`required action "actions/checkout" must use version "v5"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errs := testVisitRequiredActionsRule(t, &Config{RequiredActions: test.required}, workflow)
			if len(errs) != len(test.want) {
				t.Fatalf("got %d errors, want %d: %v", len(errs), len(test.want), errs)
			}
			for i, want := range test.want {
				if got := errs[i].Message; got != want {
					t.Errorf("error %d is %q, want %q", i, got, want)
				}
			}
		})
	}
}

func TestRuleRequiredActionsIgnoresDynamicReferences(t *testing.T) {
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@${{ inputs.version }}
`
	errs := testVisitRequiredActionsRule(t, &Config{RequiredActions: []RequiredActionRule{{Action: "actions/checkout"}}}, workflow)
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if got, want := errs[0].Message, `required action "actions/checkout" is not used in this workflow`; got != want {
		t.Fatalf("got error %q, want %q", got, want)
	}
}

func TestParseRequiredActionRef(t *testing.T) {
	tests := []struct {
		uses    string
		action  string
		version string
		ok      bool
	}{
		{uses: "actions/checkout@v4", action: "actions/checkout", version: "v4", ok: true},
		{uses: "owner/repo/path@v4", action: "owner/repo/path", version: "v4", ok: true},
		{uses: "docker://alpine:latest"},
		{uses: "./local-action"},
		{uses: "actions/checkout"},
	}
	for _, test := range tests {
		t.Run(test.uses, func(t *testing.T) {
			action, version, ok := parseRequiredActionRef(test.uses)
			if action != test.action || version != test.version || ok != test.ok {
				t.Fatalf("parseRequiredActionRef(%q) = (%q, %q, %t), want (%q, %q, %t)", test.uses, action, version, ok, test.action, test.version, test.ok)
			}
		})
	}
}
