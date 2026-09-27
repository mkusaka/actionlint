package actionlint

import "testing"

func TestRuleExplicitIfExpressions(t *testing.T) {
	pos := &Pos{Line: 3, Col: 7}

	tests := []struct {
		name    string
		enabled bool
		job     *Job
		step    *Step
		want    string
	}{
		{
			name:    "disabled",
			enabled: false,
			job:     &Job{If: &String{Value: "github.ref == 'refs/heads/main'", Pos: pos}},
			step:    &Step{If: &String{Value: "github.event_name == 'push'", Pos: pos}},
		},
		{
			name:    "bare job expression",
			enabled: true,
			job:     &Job{If: &String{Value: "github.ref == 'refs/heads/main'", Pos: pos}},
			want:    `if: expression "github.ref == 'refs/heads/main'" must be wrapped with ${{ }}`,
		},
		{
			name:    "bare step expression",
			enabled: true,
			step:    &Step{If: &String{Value: "github.event_name == 'push'", Pos: pos}},
			want:    `if: expression "github.event_name == 'push'" must be wrapped with ${{ }}`,
		},
		{
			name:    "wrapped expressions",
			enabled: true,
			job:     &Job{If: &String{Value: "${{ github.ref == 'refs/heads/main' }}", Pos: pos}},
			step:    &Step{If: &String{Value: "${{ github.event_name == 'push' }}", Pos: pos}},
		},
		{
			name:    "snapshot if is not a supported location",
			enabled: true,
			job:     &Job{Snapshot: &Snapshot{If: &String{Value: "github.ref == 'refs/heads/main'", Pos: pos}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule := NewRuleExplicitIfExpressions()
			rule.SetConfig(&Config{RequireExplicitIfExpressions: tc.enabled})
			if tc.job != nil {
				if err := rule.VisitJobPre(tc.job); err != nil {
					t.Fatal(err)
				}
			}
			if tc.step != nil {
				if err := rule.VisitStep(tc.step); err != nil {
					t.Fatal(err)
				}
			}

			errs := rule.Errs()
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("got unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) != 1 {
				t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
			}
			if err := errs[0]; err.Message != tc.want || err.Line != pos.Line || err.Column != pos.Col || err.Kind != rule.Name() {
				t.Fatalf("got error %#v", err)
			}
		})
	}
}
