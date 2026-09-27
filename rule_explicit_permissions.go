package actionlint

// RuleExplicitPermissions enforces configured least-privilege permission policies.
type RuleExplicitPermissions struct {
	RuleBase
	requireJobPermissions bool
}

// NewRuleExplicitPermissions creates a new RuleExplicitPermissions instance.
func NewRuleExplicitPermissions() *RuleExplicitPermissions {
	return &RuleExplicitPermissions{RuleBase: NewRuleBase(
		"explicit-permissions",
		"Checks configured explicit permissions policies",
	)}
}

// VisitWorkflowPre checks workflow-level permission policies.
func (rule *RuleExplicitPermissions) VisitWorkflowPre(workflow *Workflow) error {
	if rule.config == nil {
		return nil
	}
	rule.requireJobPermissions = rule.requiresNoWorkflowPermissions(workflow)

	if rule.config.RequirePermissions && workflow.Permissions == nil && !rule.requiresNoWorkflowPermissions(workflow) {
		rule.Error(workflowPolicyPos(), "workflow must explicitly set permissions")
	}
	if rule.requiresNoWorkflowPermissions(workflow) && !hasNoPermissions(workflow.Permissions) {
		pos := workflowPolicyPos()
		if workflow.Permissions != nil {
			pos = workflow.Permissions.Pos
		}
		rule.Error(pos, "workflow with multiple jobs must set permissions to {}")
	}
	return nil
}

// VisitJobPre checks each job in a multi-job workflow for an explicit permission override.
func (rule *RuleExplicitPermissions) VisitJobPre(job *Job) error {
	if rule.config == nil || !rule.requireJobPermissions {
		return nil
	}
	if job.Permissions == nil {
		rule.Error(job.Pos, "job must explicitly set permissions")
	}
	return nil
}

func (rule *RuleExplicitPermissions) requiresNoWorkflowPermissions(workflow *Workflow) bool {
	return rule.config.RequireExplicitPermissions && len(workflow.Jobs) > 1
}

func hasNoPermissions(permissions *Permissions) bool {
	if permissions == nil || permissions.All != nil {
		return false
	}
	for _, scope := range permissions.Scopes {
		if scope == nil || scope.Value == nil || scope.Value.Value != "none" {
			return false
		}
	}
	return true
}

var workflowPolicyStartPos = &Pos{Line: 1, Col: 1}

func workflowPolicyPos() *Pos {
	return workflowPolicyStartPos
}
