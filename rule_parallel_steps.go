package actionlint

import "strings"

// RuleParallelSteps checks references between background, wait, and cancel steps. It also checks that
// a parallel group contains only run and uses steps.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type RuleParallelSteps struct {
	RuleBase
	background map[string]struct{}
	inParallel map[*Step]struct{}
}

// NewRuleParallelSteps creates a new RuleParallelSteps instance.
func NewRuleParallelSteps() *RuleParallelSteps {
	return &RuleParallelSteps{
		RuleBase: RuleBase{
			name: "parallel-steps",
			desc: "Checks \"wait\"/\"cancel\" references to background steps and steps forbidden inside a \"parallel\" group",
		},
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleParallelSteps) VisitJobPre(*Job) error {
	rule.background = map[string]struct{}{}
	rule.inParallel = map[*Step]struct{}{}
	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children.
func (rule *RuleParallelSteps) VisitJobPost(*Job) error {
	rule.background = nil
	rule.inParallel = nil
	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleParallelSteps) VisitStep(step *Step) error {
	_, inParallel := rule.inParallel[step]
	switch exec := step.Exec.(type) {
	case *ExecWait:
		if !inParallel {
			for _, name := range exec.Names {
				rule.checkReference(name)
			}
		}
	case *ExecCancel:
		if !inParallel {
			rule.checkReference(exec.Name)
		}
	case *ExecParallel:
		rule.checkParallelSteps(exec.Steps)
	}

	if !inParallel && step.ID != nil && !step.ID.ContainsExpression() && isBackgroundStep(step) {
		rule.background[strings.ToLower(step.ID.Value)] = struct{}{}
	}

	return nil
}

func (rule *RuleParallelSteps) checkParallelSteps(steps []*Step) {
	for _, step := range steps {
		rule.inParallel[step] = struct{}{}
		if step.Background != nil {
			rule.Errorf(step.Background.Pos, "\"background\" is not allowed for a step inside a \"parallel\" group because the group's steps already run in the background")
		}
		switch exec := step.Exec.(type) {
		case *ExecWait:
			kind := "wait"
			if exec.All {
				kind = "wait-all"
			}
			rule.Errorf(step.Pos, "%q step is not allowed inside a \"parallel\" group", kind)
		case *ExecCancel:
			rule.Errorf(step.Pos, "\"cancel\" step is not allowed inside a \"parallel\" group")
		case *ExecParallel:
			rule.Errorf(step.Pos, "\"parallel\" step cannot be nested in another \"parallel\" step")
		}
	}
}

func (rule *RuleParallelSteps) checkReference(reference *String) {
	if reference == nil || reference.Value == "" || reference.ContainsExpression() {
		return
	}
	if _, ok := rule.background[strings.ToLower(reference.Value)]; !ok {
		rule.Errorf(
			reference.Pos,
			"%q is not the ID of a preceding background step. \"wait\" and \"cancel\" steps can only refer to an earlier step that has \"background: true\"",
			reference.Value,
		)
	}
}

func isBackgroundStep(step *Step) bool {
	return step.Background != nil && (step.Background.Expression != nil || step.Background.Value)
}
