package actionlint

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestParseActionMetadataCompositeParallelSteps(t *testing.T) {
	source := []byte(`name: Test action
description: Tests composite action parsing
inputs:
  token:
    description: Token for the action
outputs:
  result:
    description: Result from the action
    value: ${{ steps.build.outputs.result }}
runs:
  using: composite
  steps:
    - parallel:
        - id: build
          run: echo result
          shell: bash
`)

	workflow, action, format, errs := ParseFile("action.yml", source, FileAutoDetect)
	if workflow != nil || action == nil || format != FileAction {
		t.Fatalf("action metadata was not selected: workflow=%#v action=%#v format=%d", workflow, action, format)
	}
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	runs, ok := action.Runs.(*CompositeActionRuns)
	if !ok || len(runs.Steps) != 1 {
		t.Fatalf("unexpected composite runs: %#v", action.Runs)
	}
	if _, ok := runs.Steps[0].Exec.(*ExecParallel); !ok {
		t.Fatalf("parallel composite step was not preserved: %#v", runs.Steps[0].Exec)
	}
}

func TestAutoDetectTreatsWorkflowDirectoryActionYAMLAsWorkflow(t *testing.T) {
	source := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: true
`)
	workflow, action, format, errs := ParseFile(".github/workflows/action.yml", source, FileAutoDetect)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if workflow == nil || action != nil || format != FileWorkflow {
		t.Fatalf("workflow action.yml was misclassified: workflow=%#v action=%#v format=%d", workflow, action, format)
	}
}

func TestParseActionMetadataRejectsCompositeStepTimeout(t *testing.T) {
	source := []byte(`name: Test action
description: Tests composite action validation
runs:
  using: composite
  steps:
    - run: echo result
      shell: bash
      timeout-minutes: 1
`)
	_, _, _, errs := ParseFile("action.yml", source, FileAction)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "\"timeout-minutes\" is not available") {
		t.Fatalf("unexpected composite step timeout errors: %v", errs)
	}
}

func TestActionVisitorChecksCompositeStepExpressions(t *testing.T) {
	source := []byte(`name: Test action
description: Tests composite action expression checking
outputs:
  result:
    description: Result from the action
    value: ${{ steps.missing.outputs.result }}
runs:
  using: composite
  steps:
    - id: build
      run: echo result
      shell: bash
`)
	_, action, _, errs := ParseFile("action.yml", source, FileAction)
	if len(errs) != 0 {
		t.Fatal(errs)
	}

	rule := NewRuleExpression(nil, nil)
	visitor := NewVisitor()
	visitor.AddPass(rule)
	if err := visitor.VisitAction(action); err != nil {
		t.Fatal(err)
	}
	if got := rule.Errs(); len(got) != 1 || !strings.Contains(got[0].Message, "missing") {
		t.Fatalf("unexpected composite action expression errors: %v", got)
	}
}

func TestRuleActionMetadataChecksInvalidFields(t *testing.T) {
	source := []byte(`name: Test action
description: Tests action metadata validation
runs:
  using: node24
  main: index.js
  pre-if: always()
branding:
  icon: not-an-icon
  color: navy
`)
	_, action, _, errs := ParseFile("action.yml", source, FileAction)
	if len(errs) != 0 {
		t.Fatal(errs)
	}

	rule := NewRuleActionMetadata()
	visitor := NewVisitor()
	visitor.AddPass(rule)
	if err := visitor.VisitAction(action); err != nil {
		t.Fatal(err)
	}
	if got := rule.Errs(); len(got) != 3 {
		t.Fatalf("wanted three metadata errors but got %d: %v", len(got), got)
	}
}

func TestActionMetadataInputsOddMappingDoesNotPanic(t *testing.T) {
	inputs := ActionMetadataInputs{}
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "orphaned-input"},
		},
	}
	if err := inputs.UnmarshalYAML(node); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("orphaned mapping key was parsed as an input: %#v", inputs)
	}
}

func TestConditionalTriggersCheckExpressions(t *testing.T) {
	source := []byte(`on:
  push:
    if: github.event_name == 'push'
  workflow_dispatch:
    if: github.actor != ''
  repository_dispatch:
    if: github.event_name ==
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: true
`)
	workflow, errs := Parse(source)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := NewRuleExpression(nil, nil)
	if err := rule.VisitWorkflowPre(workflow); err != nil {
		t.Fatal(err)
	}
	if got := rule.Errs(); len(got) != 1 || got[0].Kind != "expression" || got[0].Line != 7 {
		t.Fatalf("invalid conditional expression must be diagnosed: %v", got)
	}
}
