package actionlint

import "testing"

func testVisitExplicitPermissionsRule(t *testing.T, cfg *Config, src string) []*Error {
	t.Helper()
	rule := NewRuleExplicitPermissions()
	rule.SetConfig(cfg)
	visitor := NewVisitor()
	visitor.AddPass(rule)
	if err := visitor.Visit(testParseWorkflow(t, src)); err != nil {
		t.Fatal(err)
	}
	return rule.Errs()
}

func TestRuleExplicitPermissionsRequirePermissions(t *testing.T) {
	const missing = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
	const explicit = `on: push
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`

	if errs := testVisitExplicitPermissionsRule(t, &Config{}, missing); len(errs) != 0 {
		t.Fatalf("disabled policy reported errors: %v", errs)
	}
	if errs := testVisitExplicitPermissionsRule(t, &Config{RequirePermissions: true}, missing); len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	} else if got, want := errs[0], "workflow must explicitly set permissions"; got.Message != want || got.Line != 1 || got.Column != 1 {
		t.Fatalf("got %#v, want message %q at 1:1", got, want)
	}
	if errs := testVisitExplicitPermissionsRule(t, &Config{RequirePermissions: true}, explicit); len(errs) != 0 {
		t.Fatalf("explicit workflow permissions reported errors: %v", errs)
	}
}

func TestRuleExplicitPermissionsMultiJobPolicy(t *testing.T) {
	const singleJob = `on: push
permissions: read-all
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
	const compliant = `on: push
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - run: echo test
  deploy:
    permissions:
      contents: read
    uses: acme/repo/.github/workflows/deploy.yml@main
`
	const noGrants = `on: push
permissions:
  contents: none
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: {}
    steps:
      - run: echo test
  deploy:
    runs-on: ubuntu-latest
    permissions: {}
    steps:
      - run: echo test
`
	const broadWorkflow = `on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - run: echo test
  deploy:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - run: echo test
`
	const missingJob = `on: push
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
  deploy:
    runs-on: ubuntu-latest
    permissions: {}
    steps:
      - run: echo test
`

	cfg := &Config{RequireExplicitPermissions: true}
	if errs := testVisitExplicitPermissionsRule(t, cfg, singleJob); len(errs) != 0 {
		t.Fatalf("single job policy reported errors: %v", errs)
	}
	if errs := testVisitExplicitPermissionsRule(t, cfg, compliant); len(errs) != 0 {
		t.Fatalf("compliant multi-job workflow reported errors: %v", errs)
	}
	if errs := testVisitExplicitPermissionsRule(t, cfg, noGrants); len(errs) != 0 {
		t.Fatalf("workflow with no granted top-level permissions reported errors: %v", errs)
	}
	if errs := testVisitExplicitPermissionsRule(t, cfg, broadWorkflow); len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	} else if got, want := errs[0], "workflow with multiple jobs must set permissions to {}"; got.Message != want || got.Line != 2 || got.Column != 1 {
		t.Fatalf("got %#v, want message %q at 2:1", got, want)
	}
	if errs := testVisitExplicitPermissionsRule(t, cfg, missingJob); len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	} else if got, want := errs[0], "job must explicitly set permissions"; got.Message != want || got.Line != 4 || got.Column != 3 {
		t.Fatalf("got %#v, want message %q at 4:3", got, want)
	}
}

func TestRuleExplicitPermissionsMultiJobMissingWorkflowPermissions(t *testing.T) {
	const workflow = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
	errs := testVisitExplicitPermissionsRule(t, &Config{RequirePermissions: true, RequireExplicitPermissions: true}, workflow)
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(errs), errs)
	}
	if got, want := errs[0].Message, "workflow with multiple jobs must set permissions to {}"; got != want {
		t.Fatalf("first error = %q, want %q", got, want)
	}
	for i, err := range errs[1:] {
		if got, want := err.Message, "job must explicitly set permissions"; got != want {
			t.Fatalf("job error %d = %q, want %q", i, got, want)
		}
	}
}
