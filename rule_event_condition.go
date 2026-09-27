package actionlint

import "strings"

// RuleEventCondition checks conditions which compare github.event_name or github.ref with workflow triggers.
type RuleEventCondition struct {
	RuleBase
	events    map[string]struct{}
	branchRef string
}

// NewRuleEventCondition creates a new RuleEventCondition instance.
func NewRuleEventCondition() *RuleEventCondition {
	return &RuleEventCondition{
		RuleBase: RuleBase{
			name: "event-condition",
			desc: "Checks github.event_name and github.ref comparisons against workflow triggers",
		},
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleEventCondition) VisitWorkflowPre(n *Workflow) error {
	rule.branchRef = ""
	rule.events = make(map[string]struct{}, len(n.On))
	for _, event := range n.On {
		rule.events[event.EventName()] = struct{}{}
	}
	// A called reusable workflow retains the caller's original event name, which
	// cannot be inferred from its own workflow_call trigger.
	if _, called := rule.events["workflow_call"]; called {
		rule.events = nil
		return nil
	}

	rule.branchRef = workflowPushBranchRef(n)

	for _, event := range n.On {
		switch event := event.(type) {
		case *WebhookEvent:
			rule.checkCondition(event.If)
		case *WorkflowDispatchEvent:
			rule.checkCondition(event.If)
		case *RepositoryDispatchEvent:
			rule.checkCondition(event.If)
		}
	}
	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleEventCondition) VisitStep(n *Step) error {
	rule.checkCondition(n.If)
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleEventCondition) VisitJobPre(n *Job) error {
	rule.checkCondition(n.If)
	if n.Snapshot != nil {
		rule.checkCondition(n.Snapshot.If)
	}
	return nil
}

func (rule *RuleEventCondition) checkCondition(condition *String) {
	if condition == nil || len(rule.events) == 0 {
		return
	}

	src := strings.TrimSpace(condition.Value)
	if strings.HasPrefix(src, "${{") && strings.HasSuffix(src, "}}") && strings.Count(src, "${{") == 1 {
		src = src[len("${{") : len(src)-len("}}")]
	}

	expr, err := NewExprParser().Parse(NewExprLexer(src + "}}"))
	if err != nil {
		return
	}
	VisitExprNode(expr, func(node, _ ExprNode, entering bool) {
		if !entering {
			return
		}
		comparison, ok := node.(*CompareOpNode)
		if !ok || !comparison.Kind.IsEqualityOp() {
			return
		}

		if eventName, ok := eventNameComparison(comparison); ok {
			if _, triggered := rule.events[eventName]; !triggered {
				if comparison.Kind == CompareOpNodeKindEq {
					rule.Errorf(condition.Pos, "comparison of github.event_name with %q is always false because the workflow does not trigger on that event", eventName)
				} else {
					rule.Errorf(condition.Pos, "comparison of github.event_name with %q is always true because the workflow does not trigger on that event", eventName)
				}
			}
		}

		if rule.branchRef != "" && comparison.Kind == CompareOpNodeKindEq && refComparison(comparison, rule.branchRef) {
			rule.Errorf(condition.Pos, "comparison of github.ref with %q is always true because the workflow only triggers on pushes to branch %q", rule.branchRef, strings.TrimPrefix(rule.branchRef, "refs/heads/"))
		}
	})
}

func workflowPushBranchRef(workflow *Workflow) string {
	if len(workflow.On) != 1 {
		return ""
	}
	event, ok := workflow.On[0].(*WebhookEvent)
	if !ok || event.Hook == nil || event.Hook.Value != "push" ||
		event.Branches == nil || len(event.Branches.Values) != 1 ||
		event.BranchesIgnore != nil || event.Tags != nil || event.TagsIgnore != nil {
		return ""
	}
	branch := event.Branches.Values[0]
	if branch == nil || branch.Value == "" || branch.ContainsExpression() ||
		strings.HasPrefix(branch.Value, "!") || strings.ContainsAny(branch.Value, "*?+[\\") {
		return ""
	}
	return "refs/heads/" + branch.Value
}

func refComparison(comparison *CompareOpNode, ref string) bool {
	return refLiteral(comparison.Left, comparison.Right, ref) ||
		refLiteral(comparison.Right, comparison.Left, ref)
}

func refLiteral(githubRef, literal ExprNode, ref string) bool {
	property, ok := githubRef.(*ObjectDerefNode)
	if !ok || property.Property != "ref" {
		return false
	}
	github, ok := property.Receiver.(*VariableNode)
	if !ok || github.Name != "github" {
		return false
	}
	value, ok := literal.(*StringNode)
	return ok && value.Value == ref
}

func eventNameComparison(comparison *CompareOpNode) (string, bool) {
	if eventName, ok := eventNameLiteral(comparison.Left, comparison.Right); ok {
		return eventName, true
	}
	return eventNameLiteral(comparison.Right, comparison.Left)
}

func eventNameLiteral(eventName, literal ExprNode) (string, bool) {
	property, ok := eventName.(*ObjectDerefNode)
	if !ok || property.Property != "event_name" {
		return "", false
	}
	github, ok := property.Receiver.(*VariableNode)
	if !ok || github.Name != "github" {
		return "", false
	}
	value, ok := literal.(*StringNode)
	if !ok {
		return "", false
	}
	return value.Value, true
}
