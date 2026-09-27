package actionlint

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLinterRemoteActionSchemasAtEachRef(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/someone/demo/v1/action.yaml":
			_, _ = io.WriteString(w, "name: Demo v1\ninputs:\n  old:\n    required: true\noutputs:\n  result:\n    description: Result\nruns:\n  using: composite\n  steps: []\n")
		case "/someone/demo/v2/action.yaml":
			_, _ = io.WriteString(w, "name: Demo v2\ninputs:\n  current:\n    required: true\noutputs:\n  next:\n    description: Next\nruns:\n  using: composite\n  steps: []\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	workflow := []byte(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: someone/demo@v1
        id: first
        with:
          old: correct
      - uses: someone/demo@v1
        with:
          old: correct
      - uses: someone/demo@v2
        with:
          old: outdated
      - run: echo '${{ steps.first.outputs.typo }}'
`)

	lint := func(fetch bool) ([]*Error, error) {
		l, err := NewLinter(io.Discard, &LinterOptions{FetchActionMetadata: fetch, ActionMetadataCacheDir: t.TempDir()})
		if err != nil {
			return nil, err
		}
		if fetch {
			l.remoteActions = newRemoteActionsCache(server.Client(), 2, t.TempDir(), "", server.URL)
		}
		return l.Lint("<stdin>", workflow, nil)
	}

	offline, err := lint(false)
	if err != nil || len(offline) != 0 {
		t.Fatalf("offline lint should not fetch or check unknown action schemas, got %v, %v", offline, err)
	}
	if requests.Load() != 0 {
		t.Fatal("offline lint made a network request")
	}

	errs, err := lint(true)
	if err != nil {
		t.Fatal(err)
	}
	var missing, unknownInput, unknownOutput bool
	for _, e := range errs {
		missing = missing || strings.Contains(e.Message, `missing input "current"`)
		unknownInput = unknownInput || strings.Contains(e.Message, `input "old" is not defined`)
		unknownOutput = unknownOutput || strings.Contains(e.Message, `property "typo" is not defined`)
	}
	if !missing || !unknownInput || !unknownOutput {
		t.Errorf("expected ref-specific input and output diagnostics, got %v", errs)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("repeated uses should fetch each ref only once: got %d requests, want 2", got)
	}
}
