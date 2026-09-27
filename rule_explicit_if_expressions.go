package actionlint

// RuleExplicitIfExpressions enforces an optional policy requiring expressions in job and step if: fields to use ${{ }} syntax.
type RuleExplicitIfExpressions struct {
	RuleBase
}

// NewRuleExplicitIfExpressions creates the explicit if expression policy rule.
func NewRuleExplicitIfExpressions() *RuleExplicitIfExpressions {
	return &RuleExplicitIfExpressions{RuleBase: RuleBase{
		name: "explicit-if-expressions",
		desc: "Checks configured use of explicit expression syntax in job and step if: fields",
	}}
}

// VisitJobPre is called before visiting a job.
func (rule *RuleExplicitIfExpressions) VisitJobPre(job *Job) error {
	rule.check(job.If)
	return nil
}

// VisitStep is called when visiting a step.
func (rule *RuleExplicitIfExpressions) VisitStep(step *Step) error {
	rule.check(step.If)
	return nil
}

func (rule *RuleExplicitIfExpressions) check(cond *String) {
	if rule.config == nil || !rule.config.RequireExplicitIfExpressions || cond == nil || cond.IsExpressionAssigned() {
		return
	}
	rule.Errorf(cond.Pos, "if: expression %q must be wrapped with ${{ }}", cond.Value)
}
