package actionlint

import (
	"slices"
	"strings"
)

const (
	permissionNone = iota
	permissionRead
	permissionWrite
)

func permissionLevel(value string) int {
	switch strings.ToLower(value) {
	case "read":
		return permissionRead
	case "write":
		return permissionWrite
	default:
		return permissionNone
	}
}

func permissionLevelName(level int) string {
	switch level {
	case permissionRead:
		return "read"
	case permissionWrite:
		return "write"
	default:
		return "none"
	}
}

func clampPermissionLevel(scope string, level int) int {
	allowed, ok := allPermissionScopes[scope]
	if !ok {
		return permissionNone
	}
	if level == permissionWrite && !slices.Contains(allowed, "write") {
		level = permissionRead
	}
	if level == permissionRead && !slices.Contains(allowed, "read") {
		return permissionNone
	}
	return level
}

// explicitPermissionLevel returns known permission for a scope. The second return value is false
// when an expression determines the permission at runtime.
func explicitPermissionLevel(p *ReusableWorkflowPermissions, scope string) (int, bool) {
	if p.Dynamic {
		return permissionNone, false
	}
	if p.All != "" {
		if ContainsExpression(p.All) {
			return permissionNone, false
		}
		switch p.All {
		case "read-all":
			return clampPermissionLevel(scope, permissionRead), true
		case "write-all":
			return clampPermissionLevel(scope, permissionWrite), true
		default:
			return permissionNone, true
		}
	}
	value, ok := p.Scopes[scope]
	if !ok {
		return permissionNone, true
	}
	if ContainsExpression(value) {
		return permissionNone, false
	}
	return clampPermissionLevel(scope, permissionLevel(value)), true
}

func defaultPermissionLevel(mode, scope string) int {
	if mode == AssumeDefaultPermissionsPermissive {
		if scope == "id-token" {
			return permissionNone
		}
		return clampPermissionLevel(scope, permissionWrite)
	}
	if scope == "contents" || scope == "packages" {
		return permissionRead
	}
	return permissionNone
}

var allPermissionScopes = map[string][]string{
	"actions":              {"read", "write", "none"},
	"artifact-metadata":    {"read", "write", "none"},
	"attestations":         {"read", "write", "none"},
	"checks":               {"read", "write", "none"},
	"code-quality":         {"read", "write", "none"},
	"contents":             {"read", "write", "none"},
	"copilot-requests":     {"write", "none"},
	"deployments":          {"read", "write", "none"},
	"discussions":          {"read", "write", "none"},
	"id-token":             {"write", "none"},
	"issues":               {"read", "write", "none"},
	"models":               {"read", "none"},
	"packages":             {"read", "write", "none"},
	"pages":                {"read", "write", "none"},
	"pull-requests":        {"read", "write", "none"},
	"repository-projects":  {"read", "write", "none"},
	"security-events":      {"read", "write", "none"},
	"statuses":             {"read", "write", "none"},
	"vulnerability-alerts": {"read", "none"},
}

// RulePermissions is a rule checker to check permission configurations in a workflow.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#defining-access-for-the-github_token-scopes
type RulePermissions struct {
	RuleBase
}

// NewRulePermissions creates new RulePermissions instance.
func NewRulePermissions() *RulePermissions {
	return &RulePermissions{
		RuleBase: RuleBase{
			name: "permissions",
			desc: "Checks for permissions configuration in \"permissions:\". Permission names and permission scopes are checked",
		},
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RulePermissions) VisitJobPre(n *Job) error {
	rule.checkPermissions(n.Permissions)
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RulePermissions) VisitWorkflowPre(n *Workflow) error {
	rule.checkPermissions(n.Permissions)
	return nil
}

func (rule *RulePermissions) checkPermissions(p *Permissions) {
	if p == nil {
		return
	}

	if p.All != nil {
		switch p.All.Value {
		case "write-all", "read-all":
			// OK
		default:
			rule.Errorf(p.All.Pos, "%q is invalid for permission for all the scopes. available values are \"read-all\", \"write-all\" or {}", p.All.Value)
		}
		return
	}

	for _, p := range p.Scopes {
		n := p.Name.Value // Permission names are case-sensitive
		s, ok := allPermissionScopes[n]
		if !ok {
			ss := make([]string, 0, len(allPermissionScopes))
			for s := range allPermissionScopes {
				ss = append(ss, s)
			}
			rule.Errorf(p.Name.Pos, "unknown permission scope %q. all available permission scopes are %s", n, sortedQuotes(ss))
			continue
		}

		if !slices.Contains(s, p.Value.Value) {
			rule.Errorf(p.Value.Pos, "%q is invalid as permission of scope %q. available values are %s", p.Value.Value, n, quotes(s))
		}
	}
}
