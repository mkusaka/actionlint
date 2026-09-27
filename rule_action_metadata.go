package actionlint

import "strings"

// RuleActionMetadata checks semantic constraints of action metadata files.
type RuleActionMetadata struct {
	RuleBase
}

// NewRuleActionMetadata creates a RuleActionMetadata.
func NewRuleActionMetadata() *RuleActionMetadata {
	return &RuleActionMetadata{RuleBase: NewRuleBase("action-metadata", "Checks action metadata fields")}
}

// VisitActionPre is callback before visiting an action metadata file.
func (rule *RuleActionMetadata) VisitActionPre(action *Action) error {
	if action.Branding != nil {
		rule.checkBranding(action.Branding)
	}
	if runs, ok := action.Runs.(*JavaScriptActionRuns); ok {
		if runs.Using != nil && runs.Using.Value == "node20" {
			rule.Errorf(runs.Using.Pos, "node20 is no longer available on github.com runners; publish this JavaScript action with runs.using: node24")
		}
		if runs.PreIf != nil && runs.Pre == nil {
			rule.Errorf(runs.PreIf.Pos, "\"pre\" is required when \"pre-if\" is specified in \"runs\" section")
		}
		if runs.PostIf != nil && runs.Post == nil {
			rule.Errorf(runs.PostIf.Pos, "\"post\" is required when \"post-if\" is specified in \"runs\" section")
		}
	}
	return nil
}

func (rule *RuleActionMetadata) checkBranding(branding *ActionBranding) {
	if branding.Icon != nil {
		if _, ok := BrandingIcons[strings.ToLower(branding.Icon.Value)]; !ok {
			rule.Errorf(branding.Icon.Pos, "invalid icon %q at \"branding.icon\"", branding.Icon.Value)
		}
	}
	if branding.Color != nil {
		if _, ok := BrandingColors[strings.ToLower(branding.Color.Value)]; !ok {
			rule.Errorf(branding.Color.Pos, "invalid color %q at \"branding.color\"", branding.Color.Value)
		}
	}
}
