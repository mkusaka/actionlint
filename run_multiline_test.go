package actionlint

import "testing"

func TestPlainMultilineRunNeedsLiteralBlock(t *testing.T) {
	for _, tc := range []struct {
		name, run string
		wantErr   bool
	}{
		{"folded commands", "run:\n          tar -czvf web.tar.gz dist\n          zip -r web.zip dist", true},
		{"literal block", "run: |\n          tar -czvf web.tar.gz dist\n          zip -r web.zip dist", false},
		{"one command below key", "run:\n          echo ok", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - " + tc.run + "\n")
			_, errs := Parse(source)
			found := false
			for _, finding := range errs {
				if finding.Line == 6 && finding.Kind == "syntax-check" {
					found = true
				}
			}
			if found != tc.wantErr {
				t.Fatalf("unexpected folded run diagnostic, want %v: %v", tc.wantErr, errs)
			}
		})
	}
}
