package actionlint

import "testing"

func TestRuleActionVersion(t *testing.T) {
	for _, tc := range []struct {
		name, uses string
		enabled    bool
		wantError  bool
	}{
		{"disabled", "owner/action@main", false, false},
		{"floating major", "owner/action@v3", true, true},
		{"branch", "owner/action@main", true, true},
		{"exact semver", "owner/action@v3.2.1", true, false},
		{"prerelease semver", "owner/action@v3.2.1-rc.1", true, false},
		{"full sha", "owner/action@012345678901234567890123456789abcdefabcd", true, false},
		{"local", "./.github/actions/local", true, false},
		{"self repository", "$/actions/local", true, false},
		{"docker", "docker://alpine:latest", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := NewRuleActionVersion()
			rule.SetConfig(&Config{RequireExactActionVersion: tc.enabled})
			step := &Step{Exec: &ExecAction{Uses: &String{Value: tc.uses, Pos: &Pos{Line: 1, Col: 1}}}}
			if err := rule.VisitStep(step); err != nil {
				t.Fatal(err)
			}
			if got := len(rule.Errs()) != 0; got != tc.wantError {
				t.Fatalf("errors for %q = %v; want error %v", tc.uses, rule.Errs(), tc.wantError)
			}
		})
	}
}
