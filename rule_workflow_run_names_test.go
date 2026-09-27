package actionlint

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWorkflowRunNamesTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, ".github", "workflows", name)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func newWorkflowRunNamesTestRule(t *testing.T, root string) *RuleWorkflowRunNames {
	t.Helper()
	project, err := NewProject(root)
	if err != nil {
		t.Fatal(err)
	}
	return NewRuleWorkflowRunNames(NewWorkflowNamesCache(project), filepath.Join(root, ".github", "workflows", "caller.yml"))
}

func workflowRunNamesTestWorkflow(names ...string) *Workflow {
	workflows := make([]*String, 0, len(names))
	for _, name := range names {
		workflows = append(workflows, &String{Value: name, Pos: &Pos{Line: 4, Col: 17}})
	}
	return &Workflow{On: []Event{&WebhookEvent{
		Hook:      &String{Value: "workflow_run", Pos: &Pos{Line: 3, Col: 3}},
		Workflows: workflows,
	}}}
}

func TestRuleWorkflowRunNamesChecksProjectWorkflows(t *testing.T) {
	root := t.TempDir()
	writeWorkflowRunNamesTestFile(t, root, "build.yaml", "name: Build\non: push\n")
	writeWorkflowRunNamesTestFile(t, root, "deploy.yaml", "name: Deploy\non: push\n")

	rule := newWorkflowRunNamesTestRule(t, root)
	if err := rule.VisitWorkflowPre(workflowRunNamesTestWorkflow("Build", "Missing", "Deploy")); err != nil {
		t.Fatal(err)
	}

	errs := rule.Errs()
	if len(errs) != 1 {
		t.Fatalf("wanted one error but got: %v", errs)
	}
	if got, want := errs[0].Message, `workflow_run event is configured for workflow "Missing" which does not exist in this project`; got != want {
		t.Errorf("wanted error %q but got %q", want, got)
	}
}

func TestRuleWorkflowRunNamesRecognizesImplicitWorkflowName(t *testing.T) {
	root := t.TempDir()
	writeWorkflowRunNamesTestFile(t, root, "nested/build.yaml", "on: push\n")

	rule := newWorkflowRunNamesTestRule(t, root)
	if err := rule.VisitWorkflowPre(workflowRunNamesTestWorkflow(".github/workflows/nested/build.yaml")); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatalf("got unexpected errors: %v", errs)
	}
}

func TestRuleWorkflowRunNamesSkipsDynamicOrIncompleteProjects(t *testing.T) {
	t.Run("dynamic name", func(t *testing.T) {
		root := t.TempDir()
		writeWorkflowRunNamesTestFile(t, root, "build.yaml", "name: Build\non: push\n")

		rule := newWorkflowRunNamesTestRule(t, root)
		if err := rule.VisitWorkflowPre(workflowRunNamesTestWorkflow("${{ github.workflow }}")); err != nil {
			t.Fatal(err)
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatalf("got unexpected errors: %v", errs)
		}
	})

	t.Run("no project", func(t *testing.T) {
		rule := NewRuleWorkflowRunNames(NewWorkflowNamesCache(nil), "")
		if err := rule.VisitWorkflowPre(workflowRunNamesTestWorkflow("Missing")); err != nil {
			t.Fatal(err)
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatalf("got unexpected errors: %v", errs)
		}
	})

	t.Run("unreadable workflow file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".github", "workflows", "unreadable.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("missing.yaml", path); err != nil {
			t.Skipf("cannot create a broken symlink: %v", err)
		}

		rule := newWorkflowRunNamesTestRule(t, root)
		if err := rule.VisitWorkflowPre(workflowRunNamesTestWorkflow("Missing")); err != nil {
			t.Fatal(err)
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatalf("got unexpected errors: %v", errs)
		}
	})
}
