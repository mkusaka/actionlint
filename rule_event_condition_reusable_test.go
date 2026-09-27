package actionlint

import "testing"

func TestRuleEventConditionReusableWorkflowInheritsCallerEvent(t *testing.T) {
	workflow := &Workflow{
		On: []Event{&WorkflowCallEvent{}},
		Jobs: map[string]*Job{
			"job": {If: &String{Value: "github.event_name == 'pull_request'", Pos: &Pos{Line: 5, Col: 5}}},
		},
	}
	rule := NewRuleEventCondition()
	visitor := NewVisitor()
	visitor.AddPass(rule)
	if err := visitor.Visit(workflow); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatalf("caller event is not known inside workflow_call: %v", errs)
	}
}
