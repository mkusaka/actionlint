package actionlint

// RuleTimeoutCheck enforces an optional job timeout policy.
type RuleTimeoutCheck struct {
	RuleBase
}

// NewRuleTimeoutCheck creates the timeout policy rule.
func NewRuleTimeoutCheck() *RuleTimeoutCheck {
	return &RuleTimeoutCheck{RuleBase: RuleBase{
		name: "timeout-check",
		desc: "Checks configured job timeout requirements and maximum values",
	}}
}

func (rule *RuleTimeoutCheck) VisitJobPre(job *Job) error {
	if rule.config == nil || job.WorkflowCall != nil {
		return nil
	}
	policy := rule.config.TimeoutMinutes
	if job.TimeoutMinutes == nil {
		if policy.Required {
			rule.Error(job.Pos, "job must set timeout-minutes")
		}
		return nil
	}
	if policy.MaxMinutes > 0 && job.TimeoutMinutes.Expression == nil && job.TimeoutMinutes.Value > policy.MaxMinutes {
		rule.Errorf(job.TimeoutMinutes.Pos, "job timeout-minutes %g exceeds configured maximum %g", job.TimeoutMinutes.Value, policy.MaxMinutes)
	}
	return nil
}
