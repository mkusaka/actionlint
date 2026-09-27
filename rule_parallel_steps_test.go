package actionlint

import "testing"

func parallelStepString(value string) *String {
	return &String{Value: value, Pos: &Pos{}}
}

func lintParallelSteps(t *testing.T, steps []*Step) []*Error {
	t.Helper()

	rule := NewRuleParallelSteps()
	visitor := NewVisitor()
	visitor.AddPass(rule)
	if err := visitor.Visit(&Workflow{Jobs: map[string]*Job{
		"test": {
			ID:    parallelStepString("test"),
			Steps: steps,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	return rule.Errs()
}

func TestRuleParallelStepsAcceptsValidBackgroundReferences(t *testing.T) {
	errs := lintParallelSteps(t, []*Step{
		{
			ID:         parallelStepString("Server"),
			Exec:       &ExecRun{},
			Background: &Bool{Value: true, Pos: &Pos{}},
		},
		{
			ID:   parallelStepString("maybe"),
			Exec: &ExecRun{},
			Background: &Bool{
				Expression: parallelStepString("${{ github.event_name == 'push' }}"),
				Pos:        &Pos{},
			},
		},
		{Exec: &ExecWait{Names: []*String{parallelStepString("server")}}},
		{Exec: &ExecWait{Names: []*String{parallelStepString("SERVER"), parallelStepString("maybe")}}},
		{Exec: &ExecWait{All: true}},
		{Exec: &ExecCancel{Name: parallelStepString("MAYBE")}},
		{Exec: &ExecCancel{Name: parallelStepString("${{ github.run_id }}")}},
		{Exec: &ExecParallel{Steps: []*Step{
			{Exec: &ExecRun{}},
			{Exec: &ExecAction{}},
		}}},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestRuleParallelStepsRejectsInvalidReferencesAndParallelChildren(t *testing.T) {
	errs := lintParallelSteps(t, []*Step{
		{ID: parallelStepString("regular"), Exec: &ExecRun{}},
		{Exec: &ExecWait{Names: []*String{parallelStepString("missing")}}},
		{Exec: &ExecCancel{Name: parallelStepString("regular")}},
		{Exec: &ExecWait{Names: []*String{parallelStepString("later")}}},
		{
			ID:         parallelStepString("later"),
			Exec:       &ExecRun{},
			Background: &Bool{Value: true, Pos: &Pos{}},
		},
		{Exec: &ExecParallel{Steps: []*Step{
			{
				ID:         parallelStepString("inner"),
				Exec:       &ExecRun{},
				Background: &Bool{Value: true, Pos: &Pos{}},
			},
			{Pos: &Pos{}, Exec: &ExecWait{Names: []*String{parallelStepString("nope")}}},
			{Pos: &Pos{}, Exec: &ExecWait{All: true}},
			{Pos: &Pos{}, Exec: &ExecCancel{Name: parallelStepString("nope")}},
			{Pos: &Pos{}, Exec: &ExecParallel{Steps: []*Step{{Exec: &ExecRun{}}}}},
		}}},
		{Exec: &ExecWait{Names: []*String{parallelStepString("inner")}}},
	})

	want := []string{
		`"missing" is not the ID of a preceding background step. "wait" and "cancel" steps can only refer to an earlier step that has "background: true"`,
		`"regular" is not the ID of a preceding background step. "wait" and "cancel" steps can only refer to an earlier step that has "background: true"`,
		`"later" is not the ID of a preceding background step. "wait" and "cancel" steps can only refer to an earlier step that has "background: true"`,
		`"background" is not allowed for a step inside a "parallel" group because the group's steps already run in the background`,
		`"wait" step is not allowed inside a "parallel" group`,
		`"wait-all" step is not allowed inside a "parallel" group`,
		`"cancel" step is not allowed inside a "parallel" group`,
		`"parallel" step cannot be nested in another "parallel" step`,
		`"inner" is not the ID of a preceding background step. "wait" and "cancel" steps can only refer to an earlier step that has "background: true"`,
	}
	if len(errs) != len(want) {
		t.Fatalf("wanted %d errors but got %d: %v", len(want), len(errs), errs)
	}
	for i, err := range errs {
		if err.Kind != "parallel-steps" || err.Message != want[i] {
			t.Errorf("error %d = [%s] %q, want [parallel-steps] %q", i, err.Kind, err.Message, want[i])
		}
	}
}
