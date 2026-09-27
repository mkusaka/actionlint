package actionlint

import (
	"fmt"
	"testing"
)

func TestCacheModeWorkflowAndJobs(t *testing.T) {
	for _, mode := range []string{"read", "write", "write-only", "none"} {
		t.Run(mode, func(t *testing.T) {
			source := fmt.Sprintf(`on: push
cache-mode: %s
jobs:
  regular:
    runs-on: ubuntu-latest
    cache-mode: %s
    steps:
      - run: echo ok
  reusable:
    uses: ./.github/workflows/build.yml
    cache-mode: %s
`, mode, mode, mode)
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatalf("valid cache modes rejected: %v", errs)
			}
			if workflow.CacheMode == nil || workflow.CacheMode.Value != mode {
				t.Fatalf("workflow cache mode not retained: %#v", workflow.CacheMode)
			}
			for _, name := range []string{"regular", "reusable"} {
				job := workflow.Jobs[name]
				if job.CacheMode == nil || job.CacheMode.Value != mode {
					t.Errorf("%s cache mode not retained: %#v", name, job.CacheMode)
				}
			}
			if workflow.Jobs["reusable"].WorkflowCall == nil {
				t.Error("cache-mode prevented reusable-workflow call parsing")
			}
		})
	}
}

func TestCacheModeRejectsUnsupportedValues(t *testing.T) {
	_, errs := Parse([]byte(`on: push
cache-mode: executable
jobs:
  test:
    runs-on: ubuntu-latest
    cache-mode: restore-only
    steps:
      - run: echo ok
`))
	if len(errs) != 2 || errs[0].Line != 2 || errs[1].Line != 6 {
		t.Fatalf("expected invalid workflow and job mode diagnostics at lines 2 and 6, got %v", errs)
	}
	for _, err := range errs {
		if err.Kind != "syntax-check" {
			t.Errorf("unexpected error kind: %v", err)
		}
	}
}
