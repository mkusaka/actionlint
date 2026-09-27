package actionlint

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.yaml.in/yaml/v4"
)

// WorkflowNamesCache caches workflow names in a project. It is safe to share between rules which
// check workflow files concurrently.
type WorkflowNamesCache struct {
	proj *Project

	once     sync.Once
	names    map[string]struct{}
	complete bool
}

// NewWorkflowNamesCache creates a cache of workflow names for the given project.
func NewWorkflowNamesCache(proj *Project) *WorkflowNamesCache {
	return &WorkflowNamesCache{proj: proj}
}

func (c *WorkflowNamesCache) get() (map[string]struct{}, bool) {
	if c == nil {
		return nil, false
	}
	c.once.Do(c.load)
	return c.names, c.complete
}

func (c *WorkflowNamesCache) load() {
	if c.proj == nil {
		return
	}

	names := map[string]struct{}{}
	err := filepath.Walk(c.proj.WorkflowsDir(), func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var metadata struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(src, &metadata); err != nil {
			return err
		}
		if strings.Contains(metadata.Name, "${{") {
			return fs.ErrInvalid
		}
		if metadata.Name == "" {
			name, err := filepath.Rel(c.proj.RootDir(), path)
			if err != nil {
				return err
			}
			names[filepath.ToSlash(name)] = struct{}{}
			return nil
		}
		names[metadata.Name] = struct{}{}
		return nil
	})
	if err != nil {
		return
	}
	c.names = names
	c.complete = true
}

// WorkflowNamesCacheFactory creates caches shared by workflow checks in each project.
type WorkflowNamesCacheFactory struct {
	caches map[string]*WorkflowNamesCache
}

// NewWorkflowNamesCacheFactory creates a factory of per-project workflow name caches.
func NewWorkflowNamesCacheFactory() *WorkflowNamesCacheFactory {
	return &WorkflowNamesCacheFactory{caches: map[string]*WorkflowNamesCache{}}
}

// GetCache returns a new or existing cache for the project. It is not safe to call concurrently.
func (f *WorkflowNamesCacheFactory) GetCache(p *Project) *WorkflowNamesCache {
	if p == nil {
		return NewWorkflowNamesCache(nil)
	}
	root := p.RootDir()
	if c, ok := f.caches[root]; ok {
		return c
	}
	c := NewWorkflowNamesCache(p)
	f.caches[root] = c
	return c
}

// RuleWorkflowRunNames checks workflow names configured at on.workflow_run.workflows.
type RuleWorkflowRunNames struct {
	RuleBase
	cache  *WorkflowNamesCache
	source string
}

// NewRuleWorkflowRunNames creates a rule which validates workflow_run workflow names in a project.
func NewRuleWorkflowRunNames(cache *WorkflowNamesCache, source string) *RuleWorkflowRunNames {
	return &RuleWorkflowRunNames{
		RuleBase: NewRuleBase(
			"workflow-run-names",
			"Checks workflow names configured at \"on.workflow_run.workflows\"",
		),
		cache:  cache,
		source: source,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleWorkflowRunNames) VisitWorkflowPre(workflow *Workflow) error {
	for _, event := range workflow.On {
		event, ok := event.(*WebhookEvent)
		if !ok || event.Hook.Value != "workflow_run" {
			continue
		}
		// A standalone workflow outside .github/workflows is not a deployed
		// member of the project's workflow set.
		if rule.cache == nil || rule.cache.proj == nil || rule.source == "" {
			return nil
		}
		dir, err := filepath.Abs(rule.cache.proj.WorkflowsDir())
		if err != nil {
			return nil
		}
		src, err := filepath.Abs(rule.source)
		if err != nil {
			return nil
		}
		relative, err := filepath.Rel(dir, src)
		if err != nil || relative == "." || !filepath.IsLocal(relative) {
			return nil
		}
		names, complete := rule.cache.get()
		if !complete {
			return nil
		}
		for _, name := range event.Workflows {
			if name.ContainsExpression() {
				continue
			}
			if _, ok := names[name.Value]; !ok {
				rule.Errorf(name.Pos, "workflow_run event is configured for workflow %q which does not exist in this project", name.Value)
			}
		}
	}
	return nil
}
