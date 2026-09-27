package actionlint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRuleWorkflowCallCheckWorkflowCallUsesFormat(t *testing.T) {
	tests := []struct {
		uses string
		ok   bool
	}{
		{"owner/repo/x.yml@ref", true},
		{"owner/repo/x.yml@@", true},
		{"owner/repo/x.yml@release/v1", true},
		{"./path/to/x.yml", true},
		{"$/path/to/x.yml", true},
		{"${{ env.FOO }}", true},
		{"./path/to/x.yml@ref", false},
		{"$/path/to/x.yml@ref", false},
		{"$/", false},
		{"$", false},
		{"/path/to/x.yml@ref", false},
		{"./", false},
		{".", false},
		{"owner/x.yml@ref", false},
		{"owner/repo@ref", false},
		{"owner/repo/x.yml", false},
		{"/repo/x.yml@ref", false},
		{"owner//x.yml@ref", false},
		{"owner/repo/@ref", false},
		{"owner/repo/x.yml@", false},
	}

	for _, tc := range tests {
		t.Run(tc.uses, func(t *testing.T) {
			c := NewLocalReusableWorkflowCache(nil, "", nil)
			r := NewRuleWorkflowCall("", c)
			j := &Job{
				WorkflowCall: &WorkflowCall{
					Uses: &String{
						Value: tc.uses,
						Pos:   &Pos{},
					},
				},
			}
			err := r.VisitJobPre(j)
			if err != nil {
				t.Fatal(err)
			}
			errs := r.Errs()
			if tc.ok && len(errs) > 0 {
				t.Fatalf("Error occurred: %v", errs)
			}
			if !tc.ok {
				if len(errs) > 2 || len(errs) == 0 {
					t.Fatalf("Wanted one error but have: %v", errs)
				}
			}
		})
	}
}

func TestRuleWorkflowCallNestedWorkflowCalls(t *testing.T) {
	w := &Workflow{
		On: []Event{
			&WorkflowCallEvent{
				Pos: &Pos{},
			},
		},
	}

	j := &Job{
		WorkflowCall: &WorkflowCall{
			Uses: &String{
				Value: "o/r/w.yaml@r",
				Pos:   &Pos{},
			},
		},
	}

	c := NewLocalReusableWorkflowCache(nil, "", nil)
	r := NewRuleWorkflowCall("", c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	if err := r.VisitJobPre(j); err != nil {
		t.Fatal(err)
	}
	errs := r.Errs()

	if len(errs) > 0 {
		t.Fatal("unexpected errors:", errs)
	}
}

func TestRuleWorkflowCallWriteEventNodeToMetadataCache(t *testing.T) {
	s := func(v string) *String {
		return &String{Value: v, Pos: &Pos{}}
	}
	w := &Workflow{
		On: []Event{
			&WorkflowCallEvent{
				Inputs: []*WorkflowCallEventInput{
					{
						Name: s("input1"),
						Type: WorkflowCallEventInputTypeString,
						ID:   "input1",
					},
				},
				Outputs: map[string]*WorkflowCallEventOutput{
					"output1": {Name: s("output1")},
				},
				Secrets: map[string]*WorkflowCallEventSecret{
					"secret1": {Name: s("secret1")},
				},
				Pos: &Pos{},
			},
		},
		Permissions: &Permissions{
			Scopes: map[string]*PermissionScope{
				"contents": {Name: s("contents"), Value: s("read")},
			},
		},
		Concurrency: &Concurrency{Group: s("workflow-group")},
		Jobs: map[string]*Job{
			"callee": {
				ID: s("callee"),
				Permissions: &Permissions{
					Scopes: map[string]*PermissionScope{
						"pull-requests": {Name: s("pull-requests"), Value: s("write")},
					},
				},
			},
		},
	}

	cwd := filepath.Join("path", "to", "project")
	c := NewLocalReusableWorkflowCache(&Project{cwd, nil}, cwd, nil)
	r := NewRuleWorkflowCall("test-workflow.yaml", c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	errs := r.Errs()
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	m, ok := c.readCache("./test-workflow.yaml")
	if !ok {
		t.Fatal("no metadata was created")
	}

	want := &ReusableWorkflowMetadata{
		Inputs: ReusableWorkflowMetadataInputs{
			"input1": {"input1", false, StringType{}},
		},
		Outputs: ReusableWorkflowMetadataOutputs{
			"output1": {"output1"},
		},
		Secrets: ReusableWorkflowMetadataSecrets{
			"secret1": {"secret1", false},
		},
		JobPermissions: map[string]*ReusableWorkflowPermissions{
			"callee": {Scopes: map[string]string{"pull-requests": "write"}},
		},
		ConcurrencyGroup: "workflow-group",
	}

	if diff := cmp.Diff(want, m); diff != "" {
		t.Fatal(diff)
	}
}

func TestRuleWorkflowCallCheckReusableWorkflowCall(t *testing.T) {
	cwd := filepath.Join("testdata", "reusable_workflow_metadata")
	cache := NewLocalReusableWorkflowCache(&Project{cwd, nil}, cwd, nil)

	for i, md := range []*ReusableWorkflowMetadata{
		// workflow0.yaml
		{
			Inputs: ReusableWorkflowMetadataInputs{
				"optional_input": {"optional_input", false, StringType{}},
				"required_input": {"required_input", true, StringType{}},
			},
			Outputs: ReusableWorkflowMetadataOutputs{
				"output": {"output"},
			},
			Secrets: ReusableWorkflowMetadataSecrets{
				"optional_secret": {"optional_secret", false},
				"required_secret": {"required_secret", true},
			},
		},
		// workflow1.yaml: Inputs and outputs in upper case (#216)
		{
			Inputs: ReusableWorkflowMetadataInputs{
				"optional_input": {"OPTIONAL_INPUT", false, StringType{}},
				"required_input": {"REQUIRED_INPUT", true, StringType{}},
			},
			Outputs: ReusableWorkflowMetadataOutputs{
				"output": {"OUTPUT"},
			},
			Secrets: ReusableWorkflowMetadataSecrets{
				"optional_secret": {"OPTIONAL_SECRET", false},
				"required_secret": {"REQUIRED_SECRET", true},
			},
		},
		// workflow2.yaml: No input and secret are defined
		{
			Inputs:  ReusableWorkflowMetadataInputs{},
			Outputs: ReusableWorkflowMetadataOutputs{},
			Secrets: ReusableWorkflowMetadataSecrets{},
		},
	} {
		cache.writeCache(fmt.Sprintf("./workflow%d.yaml", i), md)
	}

	tests := []struct {
		what           string
		uses           string
		inputs         []string
		secrets        []string
		inheritSecrets bool
		errs           []string
	}{
		{
			what:    "all",
			uses:    "./workflow0.yaml",
			inputs:  []string{"optional_input", "required_input"},
			secrets: []string{"optional_secret", "required_secret"},
		},
		{
			what:    "only required",
			uses:    "./workflow0.yaml",
			inputs:  []string{"required_input"},
			secrets: []string{"required_secret"},
		},
		{
			// The cache above was populated under "./workflow0.yaml", so resolving this proves the
			// two spellings reach one entry rather than each needing their own.
			what:    "self-repository spelling of a workflow cached as local",
			uses:    "$/workflow0.yaml",
			inputs:  []string{"required_input"},
			secrets: []string{"required_secret"},
		},
		{
			what:    "unknown workflow",
			uses:    "./unknown-workflow.yaml",
			inputs:  []string{"aaa", "bbb"},
			secrets: []string{"xxx", "yyy"},
			errs: []string{
				"could not read reusable workflow file for \"./unknown-workflow.yaml\":",
			},
		},
		{
			// The error quotes the spec as written rather than the canonical form it is looked up by.
			what:    "unknown workflow in self-repository spelling",
			uses:    "$/unknown-self-workflow.yaml",
			inputs:  []string{"aaa", "bbb"},
			secrets: []string{"xxx", "yyy"},
			errs: []string{
				"could not read reusable workflow file for \"$/unknown-self-workflow.yaml\":",
			},
		},
		{
			what:    "missing required input and secret",
			uses:    "./workflow0.yaml",
			inputs:  []string{"optional_input"},
			secrets: []string{"optional_secret"},
			errs: []string{
				"input \"required_input\" is required",
				"secret \"required_secret\" is required",
			},
		},
		{
			what:    "undefined input and secret",
			uses:    "./workflow0.yaml",
			inputs:  []string{"required_input", "unknown_input"},
			secrets: []string{"required_secret", "unknown_secret"},
			errs: []string{
				"input \"unknown_input\" is not defined in \"./workflow0.yaml\" reusable workflow. defined inputs are \"optional_input\", \"required_input\"",
				"secret \"unknown_secret\" is not defined in \"./workflow0.yaml\" reusable workflow. defined secrets are \"optional_secret\", \"required_secret\"",
			},
		},
		{
			what:           "inherit secrets",
			uses:           "./workflow0.yaml",
			inputs:         []string{"required_input"},
			secrets:        []string{"unknown_secret", "optional_secret"},
			inheritSecrets: true,
		},
		{
			what:    "read workflow",
			uses:    "./ok.yaml", // Defined in testdata/reusable_workflow_metadata/ok.yaml
			inputs:  []string{"input2"},
			secrets: []string{"secret2"},
		},
		{
			what: "read broken workflow",
			uses: "./broken.yaml", // Defined in testdata/reusable_workflow_metadata/broken.yaml
			errs: []string{
				"error while parsing reusable workflow \"./broken.yaml\"",
			},
		},
		{
			what: "external workflow call with no input and no secret",
			uses: "owner/repo/path/to/workflow@main",
		},
		{
			what:    "external workflow call with inputs and secrets",
			uses:    "owner/repo/path/to/workflow@main",
			inputs:  []string{"aaa", "bbb"},
			secrets: []string{"xxx", "yyy"},
		},
		{
			what:    "call in upper case and workflow in lower case",
			uses:    "./workflow0.yaml",
			inputs:  []string{"OPTIONAL_INPUT", "REQUIRED_INPUT"},
			secrets: []string{"OPTIONAL_SECRET", "REQUIRED_SECRET"},
		},
		{
			what:    "call in lower case and workflow in upper case",
			uses:    "./workflow1.yaml",
			inputs:  []string{"optional_input", "required_input"},
			secrets: []string{"optional_secret", "required_secret"},
		},
		{
			what:    "call in upper case and workflow in upper case",
			uses:    "./workflow1.yaml",
			inputs:  []string{"OPTIONAL_INPUT", "REQUIRED_INPUT"},
			secrets: []string{"OPTIONAL_SECRET", "REQUIRED_SECRET"},
		},
		{
			what:    "undefined upper input and secret",
			uses:    "./workflow0.yaml",
			inputs:  []string{"required_input", "UNKNOWN_INPUT"},
			secrets: []string{"required_secret", "UNKNOWN_SECRET"},
			errs: []string{
				"input \"UNKNOWN_INPUT\" is not defined in \"./workflow0.yaml\"",
				"secret \"UNKNOWN_SECRET\" is not defined in \"./workflow0.yaml\"",
			},
		},
		{
			what:    "no input and secret defined",
			uses:    "./workflow2.yaml",
			inputs:  []string{"unknown_input"},
			secrets: []string{"unknown_secret"},
			errs: []string{
				"input \"unknown_input\" is not defined in \"./workflow2.yaml\" reusable workflow. no input is defined",
				"secret \"unknown_secret\" is not defined in \"./workflow2.yaml\" reusable workflow. no secret is defined",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			r := NewRuleWorkflowCall("this-workflow.yaml", cache)

			w := &Workflow{
				On: []Event{
					&WorkflowCallEvent{
						Pos: &Pos{},
					},
				},
			}
			if err := r.VisitWorkflowPre(w); err != nil {
				t.Fatal(err)
			}

			c := &WorkflowCall{
				Uses:           &String{Value: tc.uses, Pos: &Pos{}},
				Inputs:         map[string]*WorkflowCallInput{},
				Secrets:        map[string]*WorkflowCallSecret{},
				InheritSecrets: tc.inheritSecrets,
			}
			for _, i := range tc.inputs {
				c.Inputs[strings.ToLower(i)] = &WorkflowCallInput{
					Name:  &String{Value: i, Pos: &Pos{}},
					Value: &String{Value: "", Pos: &Pos{}},
				}
			}
			for _, s := range tc.secrets {
				c.Secrets[strings.ToLower(s)] = &WorkflowCallSecret{
					Name:  &String{Value: s, Pos: &Pos{}},
					Value: &String{Value: "", Pos: &Pos{}},
				}
			}

			j := &Job{WorkflowCall: c}
			if err := r.VisitJobPre(j); err != nil {
				t.Fatal(err)
			}

			errs := []string{}
			for _, err := range r.Errs() {
				errs = append(errs, err.Error())
			}
			sort.Strings(errs)

			if len(errs) != len(tc.errs) {
				t.Fatalf(
					"Number of errors is unexpected. %d errors was expected but got %d errors. Expected errors are %v but actual errors are %v",
					len(tc.errs),
					len(errs),
					tc.errs,
					errs,
				)
			}

			for i, have := range errs {
				want := tc.errs[i]
				if !strings.Contains(have, want) {
					t.Errorf("%d-th error is unexpected. %q should be contained in error message %q", i, want, have)
				}
			}
		})
	}
}

func TestRuleWorkflowCallChecksReusableWorkflowConcurrency(t *testing.T) {
	const issueGroup = "${{ github.workflow }}-${{ github.event.pull_request.number || github.sha }}"
	tests := []struct {
		name           string
		callerGroup    string
		calleeGroup    string
		unrelatedGroup string
		wantError      bool
	}{
		{
			name:        "reports issue 538 group",
			callerGroup: issueGroup,
			calleeGroup: issueGroup,
			wantError:   true,
		},
		{
			name:        "ignores identical caller input expressions",
			callerGroup: "${{ inputs.scope }}",
			calleeGroup: "${{ inputs.scope }}",
		},
		{
			name:        "ignores github workflow ref",
			callerGroup: "${{ github.workflow_ref }}",
			calleeGroup: "${{ github.workflow_ref }}",
		},
		{
			name:        "ignores github workflow sha",
			callerGroup: "${{ github.workflow_sha }}",
			calleeGroup: "${{ github.workflow_sha }}",
		},
		{
			name:        "ignores github job workflow reference",
			callerGroup: "${{ github.job_workflow_ref }}",
			calleeGroup: "${{ github.job_workflow_ref }}",
		},
		{
			name:        "reports equal static group",
			callerGroup: "deploy",
			calleeGroup: "deploy",
			wantError:   true,
		},
		{
			name:        "ignores distinct expressions",
			callerGroup: issueGroup,
			calleeGroup: "${{ github.workflow }}-${{ github.ref }}",
		},
		{
			name:        "ignores distinct static groups",
			callerGroup: "caller",
			calleeGroup: "callee",
		},
		{
			name:           "ignores unrelated job group",
			callerGroup:    "caller",
			calleeGroup:    "callee",
			unrelatedGroup: "callee",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			unrelatedGroup := tc.unrelatedGroup
			if unrelatedGroup == "" {
				unrelatedGroup = "unrelated"
			}

			calleePath := filepath.Join(root, ".github", "workflows", "callee.yaml")
			if err := os.MkdirAll(filepath.Dir(calleePath), 0o755); err != nil {
				t.Fatal(err)
			}
			callee := fmt.Sprintf(`on: workflow_call
concurrency:
  group: %s
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo callee
`, tc.calleeGroup)
			if err := os.WriteFile(calleePath, []byte(callee), 0o600); err != nil {
				t.Fatal(err)
			}

			caller := fmt.Sprintf(`on: pull_request
concurrency:
  group: %s
jobs:
  call:
    uses: ./.github/workflows/callee.yaml
  unrelated:
    runs-on: ubuntu-latest
    concurrency:
      group: %s
    steps:
      - run: echo unrelated
`, tc.callerGroup, unrelatedGroup)
			workflow, errs := Parse([]byte(caller))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleWorkflowCall(
				filepath.Join(root, ".github", "workflows", "caller.yaml"),
				NewLocalReusableWorkflowCache(&Project{root, nil}, root, nil),
			)
			visitor := NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}

			errs = rule.Errs()
			if tc.wantError {
				if len(errs) != 1 || !strings.Contains(errs[0].Message, "may cause a deadlock") {
					t.Fatalf("wanted one deadlock error, got %v", errs)
				}
			} else if len(errs) != 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
		})
	}
}

func TestRuleWorkflowCallChecksCalleePermissions(t *testing.T) {
	permissions := func(scopes map[string]string) *ReusableWorkflowPermissions {
		return &ReusableWorkflowPermissions{Scopes: scopes}
	}
	astPermissions := func(scopes map[string]string) *Permissions {
		if scopes == nil {
			return nil
		}
		p := &Permissions{Scopes: make(map[string]*PermissionScope, len(scopes))}
		for name, value := range scopes {
			p.Scopes[name] = &PermissionScope{
				Name:  &String{Value: name, Pos: &Pos{}},
				Value: &String{Value: value, Pos: &Pos{}},
			}
		}
		return p
	}

	tests := []struct {
		name           string
		callerWorkflow map[string]string
		callerJob      map[string]string
		callee         *ReusableWorkflowPermissions
		defaultMode    string
		wantError      string
	}{
		{
			name:      "reports unavailable permission",
			callerJob: map[string]string{"pull-requests": "read"},
			callee:    permissions(map[string]string{"pull-requests": "write"}),
			wantError: `requires "pull-requests: write" but the calling job grants "pull-requests: read"`,
		},
		{
			name:      "accepts granted permission",
			callerJob: map[string]string{"pull-requests": "write"},
			callee:    permissions(map[string]string{"pull-requests": "write"}),
		},
		{
			name:      "callee inherits caller permission",
			callerJob: map[string]string{"pull-requests": "none"},
			callee:    nil,
		},
		{
			name:           "calling job overrides workflow permission",
			callerWorkflow: map[string]string{"contents": "write"},
			callerJob:      map[string]string{"contents": "read"},
			callee:         permissions(map[string]string{"contents": "write"}),
			wantError:      `requires "contents: write" but the calling job grants "contents: read"`,
		},
		{
			name:      "dynamic caller permission is not guessed",
			callerJob: map[string]string{"contents": "${{ inputs.contents_permission }}"},
			callee:    permissions(map[string]string{"contents": "write"}),
		},
		{
			name:      "dynamic callee permission is not guessed",
			callerJob: map[string]string{"contents": "read"},
			callee:    permissions(map[string]string{"contents": "${{ inputs.contents_permission }}"}),
		},
		{
			name:      "dynamic callee permission set is not guessed",
			callerJob: map[string]string{"contents": "none"},
			callee:    &ReusableWorkflowPermissions{All: "${{ inputs.permissions }}", Dynamic: true},
		},
		{
			name:   "restricted default grants contents read",
			callee: permissions(map[string]string{"contents": "read"}),
		},
		{
			name:      "restricted default denies pull requests write",
			callee:    permissions(map[string]string{"pull-requests": "write"}),
			wantError: `requires "pull-requests: write" but the calling job grants "pull-requests: none"`,
		},
		{
			name:        "permissive default grants pull requests write",
			callee:      permissions(map[string]string{"pull-requests": "write"}),
			defaultMode: AssumeDefaultPermissionsPermissive,
		},
		{
			name:        "permissive default still denies id token",
			callee:      permissions(map[string]string{"id-token": "write"}),
			defaultMode: AssumeDefaultPermissionsPermissive,
			wantError:   `requires "id-token: write" but the calling job grants "id-token: none"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewLocalReusableWorkflowCache(&Project{"testdata", nil}, "testdata", nil)
			cache.writeCache("./callee.yaml", &ReusableWorkflowMetadata{
				JobPermissions: map[string]*ReusableWorkflowPermissions{"callee": tc.callee},
			})
			rule := NewRuleWorkflowCall("caller.yaml", cache)
			if tc.defaultMode != "" {
				mode := tc.defaultMode
				rule.SetConfig(&Config{AssumeDefaultPermissions: &mode})
			}
			if err := rule.VisitWorkflowPre(&Workflow{Permissions: astPermissions(tc.callerWorkflow)}); err != nil {
				t.Fatal(err)
			}
			if err := rule.VisitJobPre(&Job{
				Permissions: astPermissions(tc.callerJob),
				WorkflowCall: &WorkflowCall{
					Uses:    &String{Value: "./callee.yaml", Pos: &Pos{}},
					Inputs:  map[string]*WorkflowCallInput{},
					Secrets: map[string]*WorkflowCallSecret{},
				},
			}); err != nil {
				t.Fatal(err)
			}
			errs := rule.Errs()
			if tc.wantError == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.wantError) {
				t.Fatalf("wanted one error containing %q, got %v", tc.wantError, errs)
			}
		})
	}
}
