package actionlint

import "testing"

func TestRuleTimeoutCheck(t *testing.T) {
	pos := &Pos{Line: 2, Col: 3}
	cases := []struct {
		name    string
		policy  TimeoutMinutesConfig
		job     *Job
		wantErr bool
	}{
		{"unset policy", TimeoutMinutesConfig{}, &Job{Pos: pos}, false},
		{"required missing", TimeoutMinutesConfig{Required: true}, &Job{Pos: pos}, true},
		{"maximum independently enforced", TimeoutMinutesConfig{MaxMinutes: 30}, &Job{Pos: pos, TimeoutMinutes: &Float{Value: 45, Pos: pos}}, true},
		{"at maximum", TimeoutMinutesConfig{MaxMinutes: 30}, &Job{Pos: pos, TimeoutMinutes: &Float{Value: 30, Pos: pos}}, false},
		{"dynamic timeout", TimeoutMinutesConfig{MaxMinutes: 30}, &Job{Pos: pos, TimeoutMinutes: &Float{Expression: &String{Value: "${{ inputs.timeout }}"}, Pos: pos}}, false},
		{"reusable call", TimeoutMinutesConfig{Required: true}, &Job{Pos: pos, WorkflowCall: &WorkflowCall{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := NewRuleTimeoutCheck()
			rule.SetConfig(&Config{TimeoutMinutes: tc.policy})
			if err := rule.VisitJobPre(tc.job); err != nil {
				t.Fatal(err)
			}
			if got := len(rule.Errs()) > 0; got != tc.wantErr {
				t.Fatalf("errors = %v, want an error: %v", rule.Errs(), tc.wantErr)
			}
		})
	}
}
