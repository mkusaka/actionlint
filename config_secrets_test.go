package actionlint

import (
	"io"
	"strings"
	"testing"
)

func TestLinterConfiguredSecrets(t *testing.T) {
	workflow := []byte("on: push\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env:\n          TOKEN: ${{ secrets.DEPLOY_TOKEN }}\n          BUILTIN: ${{ secrets.GITHUB_TOKEN }}\n")
	for _, tc := range []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"disabled", "config-secrets: null\n", false},
		{"known case insensitive", "config-secrets: [deploy_token]\n", false},
		{"unknown", "config-secrets: [OTHER_TOKEN]\n", true},
		{"empty allowlist", "config-secrets: []\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			linter, err := NewLinter(io.Discard, &LinterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			linter.defaultConfig = cfg
			errs, err := linter.Lint("missing-workflow.yaml", workflow, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Message, "secret") && (strings.Contains(e.Message, "DEPLOY_TOKEN") || strings.Contains(e.Message, "deploy_token")) {
					found = true
				}
				if strings.Contains(e.Message, "GITHUB_TOKEN") || strings.Contains(e.Message, "github_token") {
					t.Fatalf("built-in token must remain available: %v", errs)
				}
			}
			if found != tc.wantErr {
				t.Fatalf("secret allowlist error = %v, want %v; errors: %v", found, tc.wantErr, errs)
			}
		})
	}
}
