package actionlint

import (
	"regexp"
	"strings"
)

var exactActionVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
var fullActionCommit = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// RuleActionVersion enforces an optional policy against floating action tags and branches.
type RuleActionVersion struct {
	RuleBase
}

// NewRuleActionVersion creates the exact action version policy rule.
func NewRuleActionVersion() *RuleActionVersion {
	return &RuleActionVersion{RuleBase: NewRuleBase("action-version", "Checks configured exact third-party action versions")}
}

func (rule *RuleActionVersion) VisitStep(step *Step) error {
	if rule.config == nil || !rule.config.RequireExactActionVersion {
		return nil
	}
	exec, ok := step.Exec.(*ExecAction)
	if !ok || exec.Uses == nil || exec.Uses.ContainsExpression() {
		return nil
	}
	uses := exec.Uses.Value
	if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "$/") || strings.HasPrefix(uses, "docker://") {
		return nil
	}
	_, ref, ok := strings.Cut(uses, "@")
	if !ok || ref == "" || exactActionVersion.MatchString(ref) || fullActionCommit.MatchString(ref) {
		return nil
	}
	rule.Errorf(exec.Uses.Pos, "action %q must use an exact version tag or full commit SHA instead of %q", uses, ref)
	return nil
}
