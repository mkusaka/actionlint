package actionlint

import "testing"

func TestRuleEventCondition(t *testing.T) {
	tests := []struct {
		name     string
		workflow string
		want     []string
	}{
		{
			name: "event not triggered is always false",
			workflow: `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name == 'pull_request' }}
        run: echo test
`,
			want: []string{"comparison of github.event_name with \"pull_request\" is always false because the workflow does not trigger on that event"},
		},
		{
			name: "redundant event in disjunction",
			workflow: `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name == 'push' || github.event_name == 'pull_request' }}
        run: echo test
`,
			want: []string{"comparison of github.event_name with \"pull_request\" is always false because the workflow does not trigger on that event"},
		},
		{
			name: "event not triggered is always true with inequality",
			workflow: `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name != 'pull_request' }}
        run: echo test
`,
			want: []string{"comparison of github.event_name with \"pull_request\" is always true because the workflow does not trigger on that event"},
		},
		{
			name: "multiple configured events",
			workflow: `on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name == 'push' || github.event_name == 'pull_request' }}
        run: echo test
`,
		},
		{
			name: "dynamic comparison",
			workflow: `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name == env.event_name }}
        run: echo test
`,
		},
		{
			name: "multiline condition",
			workflow: `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: |
          github.event_name == 'pull_request' &&
          github.ref == 'refs/heads/main'
        run: echo test
`,
			want: []string{"comparison of github.event_name with \"pull_request\" is always false because the workflow does not trigger on that event"},
		},
		{
			name: "event trigger condition",
			workflow: `on:
  push:
    if: ${{ github.event_name == 'pull_request' }}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`,
			want: []string{"comparison of github.event_name with \"pull_request\" is always false because the workflow does not trigger on that event"},
		},
		{
			name: "single push branch makes job and step ref comparisons redundant",
			workflow: `on:
  push:
    branches: [main]
jobs:
  build:
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - if: "github.ref == 'refs/heads/main'"
        run: echo test
`,
			want: []string{
				"comparison of github.ref with \"refs/heads/main\" is always true because the workflow only triggers on pushes to branch \"main\"",
				"comparison of github.ref with \"refs/heads/main\" is always true because the workflow only triggers on pushes to branch \"main\"",
			},
		},
		{
			name: "different branch comparison",
			workflow: `on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/develop'
        run: echo test
`,
		},
		{
			name: "multiple push branches",
			workflow: `on:
  push:
    branches: [main, develop]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main'
        run: echo test
`,
		},
		{
			name: "glob push branch",
			workflow: `on:
  push:
    branches: [main*]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main'
        run: echo test
`,
		},
		{
			name: "dynamic push branch",
			workflow: `on:
  push:
    branches: ['${{ vars.branch }}']
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main'
        run: echo test
`,
		},
		{
			name: "push with tags",
			workflow: `on:
  push:
    branches: [main]
    tags: [v*]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main'
        run: echo test
`,
		},
		{
			name: "branch exclusion filter",
			workflow: `on:
  push:
    branches: [main]
    branches-ignore: [develop]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main'
        run: echo test
`,
		},
		{
			name: "multiple triggers",
			workflow: `on:
  push:
    branches: [main]
  pull_request:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main'
        run: echo test
`,
		},
		{
			name: "dynamic ref comparison",
			workflow: `on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == env.ref
        run: echo test
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := Parse([]byte(tc.workflow))
			if len(errs) != 0 {
				t.Fatal(errs)
			}

			rule := NewRuleEventCondition()
			visitor := NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}

			errs = rule.Errs()
			if len(errs) != len(tc.want) {
				t.Fatalf("wanted %d errors but got %v", len(tc.want), errs)
			}
			for i, want := range tc.want {
				if errs[i].Message != want {
					t.Errorf("unexpected error message: want %q, got %q", want, errs[i].Message)
				}
			}
		})
	}
}
