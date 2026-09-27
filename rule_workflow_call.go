package actionlint

import (
	"fmt"
	"slices"
	"strings"
)

// RuleWorkflowCall is a rule checker to check workflow call at jobs.<job_id>.
type RuleWorkflowCall struct {
	RuleBase
	workflowCallEventPos *Pos
	workflowPath         string
	cache                *LocalReusableWorkflowCache
	workflow             *Workflow
}

// NewRuleWorkflowCall creates a new RuleWorkflowCall instance. 'workflowPath' is a file path to
// the workflow which is relative to a project root directory or an absolute path.
func NewRuleWorkflowCall(workflowPath string, cache *LocalReusableWorkflowCache) *RuleWorkflowCall {
	return &RuleWorkflowCall{
		RuleBase: RuleBase{
			name: "workflow-call",
			desc: "Checks for reusable workflow calls. Inputs and outputs of called reusable workflow are checked",
		},
		workflowCallEventPos: nil,
		workflowPath:         workflowPath,
		cache:                cache,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleWorkflowCall) VisitWorkflowPre(n *Workflow) error {
	rule.workflow = n
	for _, e := range n.On {
		if e, ok := e.(*WorkflowCallEvent); ok {
			rule.workflowCallEventPos = e.Pos
			// Register this reusable workflow in cache so that it does not need to parse this workflow
			// file again when this workflow is called by other workflows.
			rule.cache.WriteWorkflowCallEventFromWorkflow(rule.workflowPath, e, n)
			break
		}
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleWorkflowCall) VisitJobPre(n *Job) error {
	if n.WorkflowCall == nil {
		return nil
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() {
		return nil
	}

	if isWorkflowCallUsesLocalFormat(u.Value) {
		rule.checkWorkflowCallUsesLocal(n, n.WorkflowCall)
		return nil
	}

	if isWorkflowCallUsesRepoFormat(u.Value) {
		return nil
	}

	if s, ok := canonLocalUsesSpec(u.Value); ok {
		// When the specification is invalid and it is local reusable workflow call, remember it caused
		// an error by setting `nil` to cache. This can prevent redundant 'could not read workflow call'
		// error.
		rule.cache.writeCache(s, nil)
	}

	rule.Errorf(
		u.Pos,
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\" nor \"./path/to/workflow.yml\" nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value,
	)
	return nil
}

func (rule *RuleWorkflowCall) checkWorkflowCallUsesLocal(caller *Job, call *WorkflowCall) {
	u := call.Uses
	m, err := rule.cache.FindMetadata(u.Value)
	if err != nil {
		rule.Error(u.Pos, err.Error())
		return
	}
	if m == nil {
		rule.Debug("Skip workflow call %q since no metadata was found", u.Value)
		return
	}

	rule.checkWorkflowCallConcurrency(call, m)

	// Validate inputs
	for n, i := range m.Inputs {
		if i != nil && i.Required {
			if _, ok := call.Inputs[n]; !ok {
				rule.Errorf(u.Pos, "input %q is required by %q reusable workflow", i.Name, u.Value)
			}
		}
	}
	for n, i := range call.Inputs {
		if _, ok := m.Inputs[n]; !ok {
			note := "no input is defined"
			if len(m.Inputs) > 0 {
				is := make([]string, 0, len(m.Inputs))
				for _, i := range m.Inputs {
					is = append(is, i.Name)
				}
				if len(is) == 1 {
					note = fmt.Sprintf("defined input is %q", is[0])
				} else {
					note = "defined inputs are " + sortedQuotes(is)
				}
			}
			rule.Errorf(i.Name.Pos, "input %q is not defined in %q reusable workflow. %s", i.Name.Value, u.Value, note)
		}
	}

	// Validate secrets
	if !call.InheritSecrets {
		for n, s := range m.Secrets {
			if s.Required {
				if _, ok := call.Secrets[n]; !ok {
					rule.Errorf(u.Pos, "secret %q is required by %q reusable workflow", s.Name, u.Value)
				}
			}
		}
		for n, s := range call.Secrets {
			if _, ok := m.Secrets[n]; !ok {
				note := "no secret is defined"
				if len(m.Secrets) > 0 {
					ss := make([]string, 0, len(m.Secrets))
					for _, s := range m.Secrets {
						ss = append(ss, s.Name)
					}
					if len(ss) == 1 {
						note = fmt.Sprintf("defined secret is %q", ss[0])
					} else {
						note = "defined secrets are " + sortedQuotes(ss)
					}
				}
				rule.Errorf(s.Name.Pos, "secret %q is not defined in %q reusable workflow. %s", s.Name.Value, u.Value, note)
			}
		}
	}

	rule.checkWorkflowCallPermissions(caller, call, m)

	rule.Debug("Validated reusable workflow %q", u.Value)
}

func (rule *RuleWorkflowCall) checkWorkflowCallConcurrency(call *WorkflowCall, metadata *ReusableWorkflowMetadata) {
	if rule.workflow == nil || rule.workflow.Concurrency == nil || rule.workflow.Concurrency.Group == nil {
		return
	}
	group := rule.workflow.Concurrency.Group
	if !workflowConcurrencyGroupsCanDeadlock(group.Value, metadata.ConcurrencyGroup) {
		return
	}
	rule.Errorf(
		group.Pos,
		"workflow concurrency group %q is also used by locally called reusable workflow %q and may cause a deadlock",
		group.Value,
		call.Uses.Value,
	)
}

func workflowConcurrencyGroupsCanDeadlock(caller, callee string) bool {
	if caller == "" || caller != callee {
		return false
	}
	if !ContainsExpression(caller) {
		return true
	}
	for {
		start := strings.Index(caller, "${{")
		if start < 0 {
			return true
		}
		src := caller[start+3:]
		lexer := NewExprLexer(src)
		expr, err := NewExprParser().Parse(lexer)
		if err != nil || !workflowConcurrencyExpressionUsesSharedContexts(expr) {
			return false
		}
		offset := lexer.Offset()
		if offset == 0 || offset > len(src) {
			return false
		}
		caller = src[offset:]
	}
}

func workflowConcurrencyExpressionUsesSharedContexts(expr ExprNode) bool {
	shared := true
	VisitExprNode(expr, func(node, _ ExprNode, entering bool) {
		if !entering || !shared {
			return
		}
		switch n := node.(type) {
		case *VariableNode:
			shared = n.Name == "github"
		case *ObjectDerefNode:
			if receiver, ok := n.Receiver.(*VariableNode); ok && receiver.Name == "github" {
				shared = sharedGitHubConcurrencyContext(n.Property)
			}
		case *IndexAccessNode:
			if workflowConcurrencyExpressionRoot(n.Operand) == "github" {
				shared = false
			}
		case *FuncCallNode:
			shared = false
		}
	})
	return shared
}

func sharedGitHubConcurrencyContext(property string) bool {
	switch property {
	case "actor", "actor_id", "base_ref", "event", "event_name", "head_ref", "ref", "ref_name", "ref_type", "repository", "repository_id", "repository_owner", "repository_owner_id", "run_attempt", "run_id", "run_number", "sha", "workflow":
		return true
	default:
		return false
	}
}

func workflowConcurrencyExpressionRoot(expr ExprNode) string {
	switch n := expr.(type) {
	case *VariableNode:
		return n.Name
	case *ObjectDerefNode:
		return workflowConcurrencyExpressionRoot(n.Receiver)
	case *ArrayDerefNode:
		return workflowConcurrencyExpressionRoot(n.Receiver)
	case *IndexAccessNode:
		return workflowConcurrencyExpressionRoot(n.Operand)
	default:
		return ""
	}
}

// checkWorkflowCallPermissions compares declared callee requirements with the caller's grant.
func (rule *RuleWorkflowCall) checkWorkflowCallPermissions(caller *Job, call *WorkflowCall, metadata *ReusableWorkflowMetadata) {
	if len(metadata.JobPermissions) == 0 {
		return
	}

	var callerPermissions *ReusableWorkflowPermissions
	if caller.Permissions != nil {
		callerPermissions = convertASTPermissions(caller.Permissions)
	} else if rule.workflow != nil {
		callerPermissions = convertASTPermissions(rule.workflow.Permissions)
	}

	mode := AssumeDefaultPermissionsRestricted
	if config := rule.Config(); config != nil && config.AssumeDefaultPermissions != nil {
		mode = *config.AssumeDefaultPermissions
	}
	callerLevel := func(scope string) (int, bool) {
		if callerPermissions == nil {
			return defaultPermissionLevel(mode, scope), true
		}
		return explicitPermissionLevel(callerPermissions, scope)
	}

	jobs := make([]string, 0, len(metadata.JobPermissions))
	for job := range metadata.JobPermissions {
		jobs = append(jobs, job)
	}
	slices.Sort(jobs)

	for _, job := range jobs {
		required := requiredPermissionLevels(metadata.JobPermissions[job])
		scopes := make([]string, 0, len(required))
		for scope := range required {
			scopes = append(scopes, scope)
		}
		slices.Sort(scopes)
		for _, scope := range scopes {
			requiredLevel := required[scope]
			grantedLevel, known := callerLevel(scope)
			if known && grantedLevel < requiredLevel {
				rule.Errorf(
					call.Uses.Pos,
					"nested job %q of %q requires %q but the calling job grants %q",
					job,
					call.Uses.Value,
					scope+": "+permissionLevelName(requiredLevel),
					scope+": "+permissionLevelName(grantedLevel),
				)
			}
		}
	}
}

func requiredPermissionLevels(permissions *ReusableWorkflowPermissions) map[string]int {
	required := map[string]int{}
	if permissions == nil || permissions.Dynamic {
		return required
	}
	if permissions.All != "" {
		if ContainsExpression(permissions.All) {
			return required
		}
		level := permissionNone
		switch permissions.All {
		case "read-all":
			level = permissionRead
		case "write-all":
			level = permissionWrite
		}
		for scope := range allPermissionScopes {
			if level := clampPermissionLevel(scope, level); level > permissionNone {
				required[scope] = level
			}
		}
		return required
	}
	for scope, value := range permissions.Scopes {
		if ContainsExpression(value) {
			continue
		}
		if level := clampPermissionLevel(scope, permissionLevel(value)); level > permissionNone {
			required[scope] = level
		}
	}
	return required
}

// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows#calling-a-reusable-workflow
func isWorkflowCallUsesLocalFormat(u string) bool {
	u, ok := canonLocalUsesSpec(u)
	if !ok {
		return false
	}
	u = strings.TrimPrefix(u, "./")

	// Cannot container a ref
	idx := strings.IndexRune(u, '@')
	if idx > 0 {
		return false
	}

	return len(u) > 0
}

// Parse {owner}/{repo}/{path to workflow.yml}@{ref}
// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows#calling-a-reusable-workflow
func isWorkflowCallUsesRepoFormat(u string) bool {
	// Repo reference must start with owner. Without the second check, "$/path/to/x.yml@ref" parses
	// as owner "$" and is accepted as a repo reference.
	if strings.HasPrefix(u, ".") || strings.HasPrefix(u, selfRepositoryUsesPrefix) {
		return false
	}

	idx := strings.IndexRune(u, '/')
	if idx <= 0 {
		return false
	}
	u = u[idx+1:] // Eat owner

	idx = strings.IndexRune(u, '/')
	if idx <= 0 {
		return false
	}
	u = u[idx+1:] // Eat repo

	idx = strings.IndexRune(u, '@')
	if idx <= 0 {
		return false
	}
	u = u[idx+1:] // Eat workflow path

	return len(u) > 0
}
