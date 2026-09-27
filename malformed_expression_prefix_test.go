package actionlint

import (
	"io"
	"testing"
)

func TestMalformedExpressionPrefixInEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value   string
		wantErr bool
	}{
		{"$ {{ vars.TOKEN }}", true},
		{"${{ vars.TOKEN }}", false},
	} {
		workflow := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env:\n      TOKEN: '" + tc.value + "'\n    steps:\n      - run: echo ok\n")
		linter, err := NewLinter(io.Discard, &LinterOptions{})
		if err != nil {
			t.Fatal(err)
		}
		errs, err := linter.Lint("workflow.yml", workflow, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, finding := range errs {
			if finding.Kind == "expression" && finding.Line == 6 {
				found = true
			}
		}
		if found != tc.wantErr {
			t.Fatalf("expression typo in %q: errors %v, want error %v", tc.value, errs, tc.wantErr)
		}
	}
}
