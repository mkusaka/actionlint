package actionlint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandMain(t *testing.T) {
	var output bytes.Buffer

	// Create command instance populating stdin/stdout/stderr
	cmd := Command{
		Stdin:  os.Stdin,
		Stdout: &output,
		Stderr: &output,
	}

	// Run the command end-to-end. Note that given args should contain program name
	workflow := filepath.Join("testdata", "examples", "main.yaml")
	status := cmd.Main([]string{"yactionlint", "-shellcheck=", "-pyflakes=", "-ignore", `label .+ is unknown\.`, workflow})

	if status != 1 {
		t.Fatal("exit status should be 1 but got", status)
	}

	out := output.String()

	for _, s := range []string{
		"main.yaml:3:5:",
		"unexpected key \"branch\" for \"push\" section",
		"^~~~~~~~~~~~~~~",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output should contain %q: %q", s, out)
		}
	}

	if strings.Contains(out, "[runner-label]") {
		t.Errorf("runner-label rule should be ignored by -ignore but it is included in output: %q", out)
	}
}

func TestCommandMainLintsActionMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "action.yml")
	if err := os.WriteFile(path, []byte("description: missing action name\nruns:\n  using: node20\n  main: index.js\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	cmd := Command{Stdout: &output, Stderr: &output}
	status := cmd.Main([]string{"yactionlint", "-shellcheck=", "-pyflakes=", path})
	if status != ExitStatusSuccessProblemFound {
		t.Fatalf("wanted action metadata lint failure status but got %d: %s", status, output.String())
	}
	if !strings.Contains(output.String(), "\"name\" is required in action metadata") {
		t.Fatalf("action metadata error was not reported: %s", output.String())
	}
}

func TestCommandMainRejectsInvalidInputFormat(t *testing.T) {
	var output bytes.Buffer
	cmd := Command{Stdout: &output, Stderr: &output}
	status := cmd.Main([]string{"yactionlint", "-input-format=unknown"})
	if status != ExitStatusInvalidCommandOption {
		t.Fatalf("wanted invalid option status but got %d", status)
	}
	if !strings.Contains(output.String(), "invalid input format") {
		t.Fatalf("invalid input format was not reported: %s", output.String())
	}
}
