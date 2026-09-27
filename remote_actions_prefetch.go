package actionlint

import (
	"strings"
	"sync"
)

// prefetchRemoteActions resolves distinct repository-action specs in parallel before
// rules visit the steps. This lets one workflow fetch different actions concurrently;
// the shared cache also deduplicates specs repeated across workflows.
func prefetchRemoteActions(workflow *Workflow, action *Action, cache *RemoteActionsCache) {
	if cache == nil {
		return
	}

	specs := map[string]struct{}{}
	var collectSteps func([]*Step)
	collectSteps = func(steps []*Step) {
		for _, step := range steps {
			switch exec := step.Exec.(type) {
			case *ExecAction:
				if exec.Uses == nil || exec.Uses.ContainsExpression() {
					continue
				}
				spec := exec.Uses.Value
				if _, local := canonLocalUsesSpec(spec); !local && !strings.HasPrefix(spec, "docker://") {
					specs[spec] = struct{}{}
				}
			case *ExecParallel:
				collectSteps(exec.Steps)
			}
		}
	}
	if workflow != nil {
		for _, job := range workflow.Jobs {
			collectSteps(job.Steps)
		}
	}
	if action != nil {
		if runs, ok := action.Runs.(*CompositeActionRuns); ok {
			collectSteps(runs.Steps)
		}
	}
	if len(specs) == 0 {
		return
	}

	jobs := make(chan string)
	var workers sync.WaitGroup
	for range min(8, len(specs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for spec := range jobs {
				// The rule reports fetch errors at each uses: position.
				_, _ = cache.FindMetadata(spec)
			}
		}()
	}
	for spec := range specs {
		jobs <- spec
	}
	close(jobs)
	workers.Wait()
}
