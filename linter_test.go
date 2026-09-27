package actionlint

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/execabs"
)

type testErrorReader struct{}

func (r testErrorReader) Read(p []byte) (int, error) {
	return 0, errors.New("dummy read error")
}

func TestLinterLintOK(t *testing.T) {
	dir := filepath.Join("testdata", "ok")

	es, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	fs := make([]string, 0, len(es))
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			fs = append(fs, filepath.Join(dir, n))
		}
	}

	proj := &Project{root: dir}
	shellcheck, _ := execabs.LookPath("shellcheck")
	pyflakes, _ := execabs.LookPath("pyflakes")

	for _, f := range fs {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if strings.Contains(f, "shellcheck") && shellcheck == "" {
				t.Skip("skipping", f, "because \"shellcheck\" command does not exist in system")
			}
			if strings.Contains(f, "pyflakes") && pyflakes == "" {
				t.Skip("skipping", f, "because \"pyflakes\" command does not exist in system")
			}

			opts := LinterOptions{
				Shellcheck: shellcheck,
				Pyflakes:   pyflakes,
			}

			linter, err := NewLinter(io.Discard, &opts)
			if err != nil {
				t.Fatal(err)
			}

			config := Config{}
			linter.defaultConfig = &config

			t.Log("Linting workflow file", f)
			errs, err := linter.LintFile(f, proj)
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) > 0 {
				t.Fatal(errs)
			}
		})
	}
}

func testFindAllWorkflowsInDir(subdir string) (string, []string, error) {
	dir := filepath.Join("testdata", subdir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}

	fs := make([]string, 0, len(entries))
	for _, info := range entries {
		if info.IsDir() {
			continue
		}
		n := info.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			fs = append(fs, filepath.Join(dir, n))
		}
	}

	return dir, fs, nil
}

func checkErrors(t *testing.T, outfile string, errs []*Error) {
	t.Helper()
	t.Logf("Checking %d errors with %q", len(errs), outfile)
	for i, err := range errs {
		t.Logf("  Error[%d]: %s", i, err.Error())
	}

	expected := []string{}
	{
		f, err := os.Open(outfile)
		if err != nil {
			panic(err)
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			expected = append(expected, s.Text())
		}
		if err := s.Err(); err != nil {
			panic(err)
		}
	}

	slices.SortFunc(errs, compareErrors)

	if len(errs) != len(expected) {
		ms := make([]string, 0, len(errs))
		for _, err := range errs {
			ms = append(ms, err.Error())
		}
		t.Fatalf("%d errors are expected but actually got %d errors:\n%v", len(expected), len(errs), strings.Join(ms, "\n"))
	}

	for i := 0; i < len(errs); i++ {
		errs[i].Filepath = filepath.ToSlash(errs[i].Filepath) // For Windows
		want, have := expected[i], errs[i].Error()
		if strings.HasPrefix(want, "/") && strings.HasSuffix(want, "/") {
			want := regexp.MustCompile(want[1 : len(want)-1])
			if !want.MatchString(have) {
				t.Errorf("error message mismatch at %dth error does not match to regular expression\n  want: /%s/\n  have: %q", i+1, want, have)
			}
		} else {
			parts := strings.SplitN(want, ":", 4)
			kindAt := strings.LastIndex(want, " [")
			if len(parts) != 4 || kindAt < 0 || !strings.HasSuffix(want, "]") {
				t.Fatalf("invalid fixture diagnostic %q", want)
			}
			line, lineErr := strconv.Atoi(parts[1])
			col, colErr := strconv.Atoi(parts[2])
			if lineErr != nil || colErr != nil {
				t.Fatalf("invalid fixture diagnostic position %q", want)
			}
			kind := want[kindAt+2 : len(want)-1]
			if parts[0] != errs[i].Filepath || line != errs[i].Line || col != errs[i].Column || kind != errs[i].Kind {
				t.Errorf("diagnostic location or kind mismatch at %d: expected %s:%d:%d [%s], got %s:%d:%d [%s]", i+1, parts[0], line, col, kind, errs[i].Filepath, errs[i].Line, errs[i].Column, errs[i].Kind)
			}
		}
	}
}

func TestLinterLintError(t *testing.T) {
	for _, subdir := range []string{"examples", "err"} {
		dir, infiles, err := testFindAllWorkflowsInDir(subdir)
		if err != nil {
			panic(err)
		}

		proj := &Project{root: dir}

		shellcheck := ""
		if p, err := execabs.LookPath("shellcheck"); err == nil {
			shellcheck = p
		}

		pyflakes := ""
		if p, err := execabs.LookPath("pyflakes"); err == nil {
			pyflakes = p
		}

		for _, infile := range infiles {
			base := strings.TrimSuffix(infile, filepath.Ext(infile))
			testName := filepath.Base(base)
			t.Run(subdir+"/"+testName, func(t *testing.T) {
				t.Log("Linting workflow file", infile)
				b, err := os.ReadFile(infile)
				if err != nil {
					panic(err)
				}

				o := LinterOptions{}

				if strings.Contains(testName, "shellcheck") {
					if shellcheck == "" {
						t.Skip("skipped because \"shellcheck\" command does not exist in system")
					}
					o.Shellcheck = shellcheck
				}

				if strings.Contains(testName, "pyflakes") {
					if pyflakes == "" {
						t.Skip("skipped because \"pyflakes\" command does not exist in system")
					}
					o.Pyflakes = pyflakes
				}

				l, err := NewLinter(io.Discard, &o)
				if err != nil {
					t.Fatal(err)
				}

				l.defaultConfig = &Config{}

				errs, err := l.Lint("test.yaml", b, proj)
				if err != nil {
					t.Fatal(err)
				}

				checkErrors(t, base+".out", errs)
			})
		}
	}
}

func TestLinterLintAllErrorWorkflowsAtOnce(t *testing.T) {
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		t.Skipf("shellcheck is not found: %s", err)
	}

	pyflakes, err := execabs.LookPath("pyflakes")
	if err != nil {
		t.Skipf("pyflakes is not found: %s", err)
	}

	dir, files, err := testFindAllWorkflowsInDir("examples")
	if err != nil {
		panic(err)
	}

	// Root directory must be testdata/examples/ since some workflow assumes it
	proj := &Project{root: dir}

	_, fs, err := testFindAllWorkflowsInDir("err")
	if err != nil {
		panic(err)
	}
	files = append(files, fs...)

	o := LinterOptions{
		Shellcheck: shellcheck,
		Pyflakes:   pyflakes,
	}

	l, err := NewLinter(io.Discard, &o)
	if err != nil {
		t.Fatal(err)
	}

	l.defaultConfig = &Config{}

	errs, err := l.LintFiles(files, proj)
	if err != nil {
		t.Fatal(err)
	}

	// Check each example workflow file caused at least one error
CheckFiles:
	for _, f := range files {
		for _, e := range errs {
			if e.Filepath == f {
				continue CheckFiles
			}
		}
		if !strings.Contains(f, "pyflakes") && !strings.Contains(f, "shellcheck") {
			t.Errorf("Workflow %q caused no error: %v", f, errs)
		}
	}
}

func TestLintFindProjectFromPath(t *testing.T) {
	d := filepath.Join("testdata", "find_project")
	f := filepath.Join(d, ".github", "workflows", "test.yaml")
	b, err := os.ReadFile(f)
	if err != nil {
		panic(err)
	}

	testEnsureDotGitDir(d)

	lint := func(path string) []*Error {
		l, err := NewLinter(io.Discard, &LinterOptions{})
		if err != nil {
			t.Fatal(err)
		}
		l.defaultConfig = &Config{}
		errs, err := l.Lint(path, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		return errs
	}

	errs := lint(f)
	if len(errs) == 0 {
		t.Fatal("no error was detected though the project was found from path parameter")
	}

	errs = lint("<stdin>")
	if len(errs) > 0 {
		t.Fatal("some error was detected though path parameter is stdin", errs)
	}

	errs = lint(filepath.Join("this-dir-doesnt-exist", "not-found.yaml"))
	if len(errs) > 0 {
		t.Fatal("some error was detected though path parameter does not exist", errs)
	}
}

func TestLinterLintProject(t *testing.T) {
	root := filepath.Join("testdata", "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		panic(err)
	}

	for _, info := range entries {
		if !info.IsDir() {
			continue
		}

		name := info.Name()
		t.Run("project/"+name, func(t *testing.T) {
			repo := filepath.Join(root, name)
			t.Log("Linting project at", repo)

			opts := LinterOptions{
				WorkingDir: repo,
			}
			cfg := filepath.Join(repo, "yactionlint.yaml")
			if _, err := os.Stat(cfg); err == nil {
				opts.ConfigFile = cfg
			}
			linter, err := NewLinter(io.Discard, &opts)
			if err != nil {
				t.Fatal(err)
			}

			proj := &Project{root: repo}
			errs, err := linter.LintDir(filepath.Join(repo, "workflows"), proj)
			if err != nil {
				t.Fatal(err)
			}

			checkErrors(t, repo+".out", errs)
		})
	}
}

func TestLinterSARIFStructure(t *testing.T) {
	dir := filepath.Join("testdata", "format")
	format, err := os.ReadFile(filepath.Join(dir, "sarif_template.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	linter, err := NewLinter(&output, &LinterOptions{Format: string(format)})
	if err != nil {
		t.Fatal(err)
	}
	linter.defaultConfig = &Config{}
	if _, err := linter.LintFile(filepath.Join(dir, "test.yaml"), &Project{root: dir}); err != nil {
		t.Fatal(err)
	}
	var sarif struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					InformationURI string `json:"informationUri"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Locations []struct {
					PhysicalLocation struct {
						Region struct {
							StartLine   int `json:"startLine"`
							StartColumn int `json:"startColumn"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(output.Bytes(), &sarif); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	if sarif.Version != "2.1.0" || len(sarif.Runs) != 1 || sarif.Runs[0].Tool.Driver.InformationURI != "https://github.com/mkusaka/yactionlint" {
		t.Fatalf("incorrect SARIF envelope: %+v", sarif)
	}
	results := sarif.Runs[0].Results
	if len(results) != 3 || results[0].RuleID != "syntax-check" || results[1].RuleID != "expression" || results[2].RuleID != "syntax-check" {
		t.Fatalf("incorrect SARIF rule results: %+v", results)
	}
	for index, want := range []struct{ line, col int }{{3, 5}, {9, 23}, {10, 9}} {
		if len(results[index].Locations) != 1 {
			t.Fatalf("result %d has incorrect locations: %+v", index, results[index].Locations)
		}
		got := results[index].Locations[0].PhysicalLocation.Region
		if got.StartLine != want.line || got.StartColumn != want.col {
			t.Fatalf("result %d location = %d:%d, want %d:%d", index, got.StartLine, got.StartColumn, want.line, want.col)
		}
	}
}

func TestLinterLintStdinOK(t *testing.T) {
	for _, f := range []string{"", "foo.yaml"} {
		l, err := NewLinter(io.Discard, &LinterOptions{StdinFileName: f})
		if err != nil {
			t.Fatalf("creating Linter object with stdin file name %q caused error: %v", f, err)
		}

		in := []byte(`on: push
jobs:
  job:
    runs-on: foo
	steps:
	  - run: echo`)
		errs, err := l.LintStdin(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("linting input with stdin file name %q caused error: %v", f, err)
		}
		if len(errs) != 1 {
			t.Fatalf("unexpected number of errors with stdin file name %q: %v", f, errs)
		}

		want := f
		if want == "" {
			want = "<stdin>"
		}
		if errs[0].Filepath != want {
			t.Fatalf("file path in the error with stdin file name %q should be %q but got %q", f, want, errs[0].Filepath)
		}
	}
}

func TestLinterLintStdinReadError(t *testing.T) {
	l, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.LintStdin(testErrorReader{})
	if err == nil {
		t.Fatal("error did not occur")
	}
	want := "could not read stdin: dummy read error"
	have := err.Error()
	if want != have {
		t.Fatalf("wanted error message %q but have %q", want, have)
	}
}

func TestLinterPathsNotFound(t *testing.T) {
	l, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	paths := []string{
		filepath.Join("testdata", "this-file-doesnt-exist.yaml"),                   // Relative file path (parent exists)
		filepath.Join("this-dir-doesnt-exist", "this-file-doesnt-exist.yaml"),      // Relative file path (parent doesn't exist)
		filepath.Join(cwd, "this-file-doesnt-exist.yaml"),                          // Absolute file path (parent exists)
		filepath.Join(cwd, "this-dir-doesnt-exist", "this-file-doesnt-exist.yaml"), // Absolute file path (parent doesn't exist)
	}

	_, err = l.LintFiles(paths, nil)
	if err == nil {
		t.Fatal("no error happened")
	}
	msg := err.Error()
	if !strings.Contains(msg, "could not read") {
		t.Fatal("unexpected error:", msg)
	}
}

type customRuleForTest struct {
	RuleBase
	count int
}

func (r *customRuleForTest) VisitStep(n *Step) error {
	r.count++
	if r.count > 1 {
		r.Errorf(n.Pos, "only single step is allowed but got %d steps", r.count)
	}
	return nil
}

func TestLinterAddCustomRuleOnRulesCreatedHook(t *testing.T) {
	o := &LinterOptions{
		OnRulesCreated: func(rules []Rule) []Rule {
			r := &customRuleForTest{
				RuleBase: NewRuleBase("this-is-test", ""),
			}
			return append(rules, r)
		},
	}

	l, err := NewLinter(io.Discard, o)
	if err != nil {
		t.Fatal(err)
	}
	l.defaultConfig = &Config{}

	{
		w := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
`
		errs, err := l.Lint("test.yaml", []byte(w), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 0 {
			t.Fatal("wanted no error but have", errs)
		}
	}

	{
		w := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
      - run: echo
`
		errs, err := l.Lint("test.yaml", []byte(w), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 1 {
			t.Fatal("wanted 1 error but have", errs)
		}

		var b strings.Builder
		errs[0].PrettyPrint(&b, nil)
		have := b.String()
		want := "test.yaml:7:9: only single step is allowed but got 2 steps [this-is-test]\n"
		if have != want {
			t.Fatalf("wanted error message %q but have %q", want, have)
		}
	}
}

func TestLinterRemoveRuleOnRulesCreatedHook(t *testing.T) {
	o := &LinterOptions{
		OnRulesCreated: func(rules []Rule) []Rule {
			for i, r := range rules {
				if r.Name() == "runner-label" {
					rules = append(rules[:i], rules[i+1:]...)
					break
				}
			}
			return rules
		},
	}

	l, err := NewLinter(io.Discard, o)
	if err != nil {
		t.Fatal(err)
	}
	l.defaultConfig = &Config{}

	f := filepath.Join("testdata", "err", "invalid_runner_labels.yaml")
	errs, err := l.LintFile(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatal("no error was expected because runner-label rule was removed but got:", errs)
	}
}

func TestLinterGenerateDefaultConfigAlreadyExists(t *testing.T) {
	l, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range []string{"ok", "yml"} {
		d := filepath.Join("testdata", "config", "projects", n)
		testEnsureDotGitDir(d)

		err := l.GenerateDefaultConfig(d)
		if err == nil {
			t.Fatal("error did not occur for project", d)
		}
		want := "config file already exists at"
		msg := err.Error()
		if !strings.Contains(msg, want) {
			t.Fatalf("error message %q does not contain expected text %q", msg, want)
		}
	}
}

func BenchmarkLintWorkflowFiles(b *testing.B) {
	scripts := filepath.Join("testdata", "bench", "many_scripts.yaml")
	small := filepath.Join("testdata", "bench", "small.yaml")
	large := filepath.Join("testdata", "bench", "large.yaml")
	min := filepath.Join("testdata", "bench", "minimal.yaml")
	proj := &Project{root: "."}
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		b.Skipf("shellcheck is not found: %s", err)
	}
	format := "{{range $ := .}}### Error at line {{$.Line}}, col {{$.Column}} of `{{$.Filepath}}`\\n\\n{{$.Message}}\\n\\n```\\n{{$.Snippet}}\\n```\\n\\n{{end}}"

	// Workflow files for this repository
	ours := []string{}
	{
		d := filepath.Join(".github", "workflows")
		es, err := os.ReadDir(d)
		if err != nil {
			panic(err)
		}
		for _, e := range es {
			ours = append(ours, filepath.Join(d, e.Name()))
		}
	}

	bms := []struct {
		what       string
		files      []string
		shellcheck string
		format     string
	}{
		{
			what:  "minimal",
			files: []string{min},
		},
		{
			what:  "minimal",
			files: []string{min, min, min, min, min},
		},
		{
			what:  "minimal",
			files: []string{min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min},
		},
		{
			what: "minimal",
			files: []string{
				min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min,
				min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min, min,
			},
		},
		{
			what:  "small",
			files: []string{small},
		},
		{
			what:  "smalls",
			files: []string{small, small, small},
		},
		{
			what:  "many-smalls",
			files: []string{small, small, small, small, small, small, small, small, small, small},
		},
		{
			what:  "large",
			files: []string{large},
		},
		{
			what:  "larges",
			files: []string{large, large, large},
		},
		{
			what:  "many-larges",
			files: []string{large, large, large, large, large, large, large, large, large, large},
		},
		{
			what:       "small",
			files:      []string{small},
			shellcheck: shellcheck,
		},
		{
			what:       "smalls",
			files:      []string{small, small, small},
			shellcheck: shellcheck,
		},
		{
			what:       "many-smalls",
			files:      []string{small, small, small, small, small, small, small, small, small, small},
			shellcheck: shellcheck,
		},
		{
			what:       "script",
			files:      []string{scripts},
			shellcheck: shellcheck,
		},
		{
			what:       "scripts",
			files:      []string{scripts, scripts, scripts},
			shellcheck: shellcheck,
		},
		{
			what:       "many-scripts",
			files:      []string{scripts, scripts, scripts, scripts, scripts, scripts, scripts, scripts, scripts, scripts},
			shellcheck: shellcheck,
		},
		{
			what:   "many-smalls",
			files:  []string{small, small, small, small, small, small, small, small, small, small},
			format: format,
		},
		{
			what:  "our workflows",
			files: ours,
		},
	}

	for _, bm := range bms {
		sc := ""
		if bm.shellcheck != "" {
			sc = "-shellcheck"
		}
		fm := ""
		if bm.format != "" {
			fm = "-format"
		}
		b.Run(fmt.Sprintf("%s%s%s-%d", bm.what, sc, fm, len(bm.files)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				opts := LinterOptions{
					Shellcheck: bm.shellcheck,
					Format:     bm.format,
				}

				l, err := NewLinter(io.Discard, &opts)
				if err != nil {
					b.Fatal(err)
				}
				l.defaultConfig = &Config{}

				errs, err := l.LintFiles(bm.files, proj)
				if err != nil {
					b.Fatal(err)
				}

				if len(errs) > 0 {
					b.Fatal("some error occurred:", errs)
				}
			}
		})
	}
}

func BenchmarkLintWorkflowContent(b *testing.B) {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	proj := &Project{root: dir}

	// Measure performance of traversing with checks except for external process rules (shellcheck, pyflakes)
	// Reading file content is not included in benchmark measurement.

	for _, name := range []string{"minimal", "small", "large"} {
		var f string
		switch name {
		case "minimal":
			f = filepath.Join(dir, "testdata", "bench", "minimal.yaml")
		case "small":
			f = filepath.Join(dir, "testdata", "bench", "small.yaml")
		case "large":
			f = filepath.Join(dir, "testdata", "bench", "many_scripts.yaml")
		}
		content, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				opts := LinterOptions{}
				l, err := NewLinter(io.Discard, &opts)
				if err != nil {
					b.Fatal(err)
				}
				l.defaultConfig = &Config{}
				errs, err := l.Lint(f, content, proj)
				if err != nil {
					b.Fatal(err)
				}
				if len(errs) > 0 {
					b.Fatal("some error occurred:", errs)
				}
			}
		})
	}
}

func BenchmarkExamplesLintFiles(b *testing.B) {
	dir, files, err := testFindAllWorkflowsInDir("examples")
	if err != nil {
		panic(err)
	}

	proj := &Project{root: dir}
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		b.Skipf("shellcheck is not found: %s", err)
	}
	pyflakes, err := execabs.LookPath("pyflakes")
	if err != nil {
		b.Skipf("pyflakes is not found: %s", err)
	}

	for i := 0; i < b.N; i++ {
		opts := LinterOptions{
			Shellcheck: shellcheck,
			Pyflakes:   pyflakes,
		}

		l, err := NewLinter(io.Discard, &opts)
		if err != nil {
			b.Fatal(err)
		}
		l.defaultConfig = &Config{}

		errs, err := l.LintFiles(files, proj)
		if err != nil {
			b.Fatal(err)
		}

		if len(errs) == 0 {
			b.Fatal("no error found")
		}
	}
}

func BenchmarkLintRepository(b *testing.B) {
	for i := 0; i < b.N; i++ {
		opts := LinterOptions{}
		l, err := NewLinter(io.Discard, &opts)
		if err != nil {
			b.Fatal(err)
		}
		errs, err := l.LintRepository(".")
		if err != nil {
			b.Fatal(err)
		}
		if len(errs) > 0 {
			b.Fatal(errs)
		}
	}
}
