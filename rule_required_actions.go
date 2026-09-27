package actionlint

import "strings"

// RequiredActionRule specifies an action which must be used in each workflow.
// Version, when non-empty, must exactly match at least one use of Action.
type RequiredActionRule struct {
	Action  string `yaml:"action"`
	Version string `yaml:"version"`
}

// RuleRequiredActions checks that configured actions are used in a workflow.
type RuleRequiredActions struct {
	RuleBase
	used map[string]map[string]struct{}
}

// NewRuleRequiredActions creates a new RuleRequiredActions instance.
func NewRuleRequiredActions() *RuleRequiredActions {
	return &RuleRequiredActions{
		RuleBase: NewRuleBase(
			"required-actions",
			"Checks that configured actions are used in workflows",
		),
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleRequiredActions) VisitWorkflowPre(*Workflow) error {
	rule.used = make(map[string]map[string]struct{})
	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleRequiredActions) VisitStep(n *Step) error {
	if rule.config == nil || len(rule.config.RequiredActions) == 0 {
		return nil
	}

	e, ok := n.Exec.(*ExecAction)
	if !ok || e.Uses == nil || e.Uses.ContainsExpression() {
		return nil
	}

	action, version, ok := parseRequiredActionRef(e.Uses.Value)
	if !ok {
		return nil
	}
	versions := rule.used[action]
	if versions == nil {
		versions = make(map[string]struct{})
		rule.used[action] = versions
	}
	versions[version] = struct{}{}
	return nil
}

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children.
func (rule *RuleRequiredActions) VisitWorkflowPost(w *Workflow) error {
	if rule.config == nil || len(rule.config.RequiredActions) == 0 {
		return nil
	}

	pos := &Pos{Line: 1, Col: 1}
	for _, job := range w.Jobs {
		if job.Pos != nil {
			pos = job.Pos
			break
		}
	}
	for _, required := range rule.config.RequiredActions {
		versions, found := rule.used[required.Action]
		if !found {
			rule.Errorf(pos, "required action %q is not used in this workflow", required.Action)
			continue
		}
		if required.Version != "" {
			if _, ok := versions[required.Version]; !ok {
				rule.Errorf(pos, "required action %q must use version %q", required.Action, required.Version)
			}
		}
	}
	return nil
}

func parseRequiredActionRef(uses string) (string, string, bool) {
	if strings.HasPrefix(uses, "docker://") {
		return "", "", false
	}
	action, version, ok := strings.Cut(uses, "@")
	if !ok || action == "" || version == "" || !strings.Contains(action, "/") {
		return "", "", false
	}
	return action, version, true
}
