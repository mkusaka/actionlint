package actionlint

import (
	"path"
	"strings"
)

type runnerOSCompat uint

const (
	compatInvalid                   = 0
	compatUbuntu2204 runnerOSCompat = 1 << iota
	compatUbuntu2404
	compatUbuntu2604
	compatMacOS140
	compatMacOS140L
	compatMacOS140XL
	compatMacOS150
	compatMacOS150Intel
	compatMacOS150L
	compatMacOS150XL
	compatMacOS260
	compatMacOS260Intel
	compatMacOS260L
	compatMacOS260XL
	compatMacOS270
	compatMacOS270XL
	compatWindows2022
	compatWindows2025
	compatWindows2025VS2026
	compatWindows11Arm
	compatWindows11VS2026Arm
)

// https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners
var allGitHubHostedRunnerLabels = []string{
	"windows-latest",
	"windows-latest-8-cores",
	"windows-2025",
	"windows-2025-vs2026",
	"windows-2022",
	"windows-11-arm",
	"windows-11-vs2026-arm",
	"ubuntu-slim",
	"ubuntu-latest",
	"ubuntu-latest-4-cores",
	"ubuntu-latest-8-cores",
	"ubuntu-latest-16-cores",
	"ubuntu-26.04",
	"ubuntu-26.04-arm",
	"ubuntu-24.04",
	"ubuntu-24.04-arm",
	"ubuntu-22.04",
	"ubuntu-22.04-arm",
	"macos-latest",
	"macos-latest-xlarge",
	"macos-latest-large",
	"macos-26-intel",
	"macos-26-xlarge",
	"macos-26-large",
	"macos-26",
	"xcode-27-xlarge",
	"xcode-27",
	"macos-15-intel",
	"macos-15-xlarge",
	"macos-15-large",
	"macos-15",
	"macos-14-xlarge",
	"macos-14-large",
	"macos-14",
}

// https://docs.github.com/en/actions/hosting-your-own-runners/using-self-hosted-runners-in-a-workflow#using-default-labels-to-route-jobs
var selfHostedRunnerPresetOSLabels = []string{
	"linux",
	"macos",
	"windows",
}

// https://docs.github.com/en/actions/hosting-your-own-runners/using-self-hosted-runners-in-a-workflow#using-default-labels-to-route-jobs
var selfHostedRunnerPresetOtherLabels = []string{
	"self-hosted",
	"x64",
	"arm",
	"arm64",
}

var defaultRunnerOSCompats = map[string]runnerOSCompat{
	"ubuntu-slim":            compatUbuntu2404,
	"ubuntu-latest":          compatUbuntu2404,
	"ubuntu-latest-4-cores":  compatUbuntu2404,
	"ubuntu-latest-8-cores":  compatUbuntu2404,
	"ubuntu-latest-16-cores": compatUbuntu2404,
	"ubuntu-26.04":           compatUbuntu2604,
	"ubuntu-26.04-arm":       compatUbuntu2604,
	"ubuntu-24.04":           compatUbuntu2404,
	"ubuntu-24.04-arm":       compatUbuntu2404,
	"ubuntu-22.04":           compatUbuntu2204,
	"ubuntu-22.04-arm":       compatUbuntu2204,
	"macos-latest-xlarge":    compatMacOS150XL,
	"macos-latest-large":     compatMacOS150L,
	"macos-latest":           compatMacOS150,
	"macos-26-intel":         compatMacOS260Intel,
	"macos-26-xlarge":        compatMacOS260XL,
	"macos-26-large":         compatMacOS260L,
	"macos-26":               compatMacOS260,
	"xcode-27-xlarge":        compatMacOS270XL,
	"xcode-27":               compatMacOS270,
	"macos-15-intel":         compatMacOS150Intel,
	"macos-15-xlarge":        compatMacOS150XL,
	"macos-15-large":         compatMacOS150L,
	"macos-15":               compatMacOS150,
	"macos-14-xlarge":        compatMacOS140XL,
	"macos-14-large":         compatMacOS140L,
	"macos-14":               compatMacOS140,
	"windows-latest":         compatWindows2022,
	"windows-latest-8-cores": compatWindows2022,
	"windows-2025":           compatWindows2025,
	"windows-2025-vs2026":    compatWindows2025VS2026,
	"windows-2022":           compatWindows2022,
	"windows-11-arm":         compatWindows11Arm,
	"windows-11-vs2026-arm":  compatWindows11VS2026Arm,
	"linux":                  compatUbuntu2604 | compatUbuntu2404 | compatUbuntu2204, // Note: "linux" does not always indicate Ubuntu. It might be Fedora or Arch or ...
	"macos":                  compatMacOS270 | compatMacOS270XL | compatMacOS260 | compatMacOS260Intel | compatMacOS260L | compatMacOS260XL | compatMacOS150 | compatMacOS150Intel | compatMacOS150L | compatMacOS150XL | compatMacOS140 | compatMacOS140L | compatMacOS140XL,
	"windows":                compatWindows2025VS2026 | compatWindows2025 | compatWindows2022 | compatWindows11Arm | compatWindows11VS2026Arm,
}

// RuleRunnerLabel is a rule to check runner label like "ubuntu-latest". There are two types of
// runners, GitHub-hosted runner and Self-hosted runner. GitHub-hosted runner is described at
// https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners .
// And Self-hosted runner is described at
// https://docs.github.com/en/actions/hosting-your-own-runners/using-self-hosted-runners-in-a-workflow .
type RuleRunnerLabel struct {
	RuleBase
	// Note: Using only one compatibility integer is enough to check compatibility. But we remember
	// all past compatibility values here for better error message. If accumulating all compatibility
	// values into one integer, we can no longer know what labels are conflicting.
	compats map[runnerOSCompat]*String
}

// NewRuleRunnerLabel creates new RuleRunnerLabel instance.
func NewRuleRunnerLabel() *RuleRunnerLabel {
	return &RuleRunnerLabel{
		RuleBase: RuleBase{
			name: "runner-label",
			desc: "Checks for GitHub-hosted and preset self-hosted runner labels in \"runs-on:\"",
		},
		compats: nil,
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleRunnerLabel) VisitJobPre(n *Job) error {
	if n.RunsOn == nil {
		return nil
	}

	var m *Matrix
	if n.Strategy != nil {
		m = n.Strategy.Matrix
	}

	if len(n.RunsOn.Labels) == 1 {
		rule.checkLabel(n.RunsOn.Labels[0], m)
		return nil
	}

	rule.compats = map[runnerOSCompat]*String{}
	if n.RunsOn.LabelsExpr != nil {
		rule.checkLabelAndConflict(n.RunsOn.LabelsExpr, m)
	} else {
		for _, label := range n.RunsOn.Labels {
			rule.checkLabelAndConflict(label, m)
		}
	}

	rule.compats = nil // reset
	return nil
}

// https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners
func (rule *RuleRunnerLabel) checkLabelAndConflict(l *String, m *Matrix) {
	if l.ContainsExpression() {
		ss := rule.tryToGetLabelsInMatrix(l, m)
		cs := make([]runnerOSCompat, 0, len(ss))
		for _, s := range ss {
			comp := rule.verifyRunnerLabel(s)
			cs = append(cs, comp)
		}
		rule.checkCombiCompat(cs, ss)
		return
	}

	comp := rule.verifyRunnerLabel(l)
	rule.checkCompat(comp, l)
}

func (rule *RuleRunnerLabel) checkLabel(l *String, m *Matrix) {
	if l.ContainsExpression() {
		ss := rule.tryToGetLabelsInMatrix(l, m)
		for _, s := range ss {
			rule.verifyRunnerLabel(s)
		}
		return
	}

	rule.verifyRunnerLabel(l)
}

func (rule *RuleRunnerLabel) verifyRunnerLabel(label *String) runnerOSCompat {
	l := label.Value
	if c, ok := defaultRunnerOSCompats[strings.ToLower(l)]; ok {
		return c
	}

	for _, p := range selfHostedRunnerPresetOtherLabels {
		if strings.EqualFold(l, p) {
			return compatInvalid
		}
	}

	known := rule.getKnownLabels()
	for _, k := range known {
		m, err := path.Match(k, l)
		if err != nil {
			rule.Errorf(label.Pos, "label pattern %q is an invalid glob. kindly check list of labels in yactionlint.yaml config file: %v", k, err)
			return compatInvalid
		}
		if m {
			return compatInvalid
		}
	}

	rule.Errorf(
		label.Pos,
		"label %q is unknown. available labels are %s. if it is a custom label for self-hosted runner, set list of labels in yactionlint.yaml config file",
		label.Value,
		quotesAll(
			allGitHubHostedRunnerLabels,
			selfHostedRunnerPresetOtherLabels,
			selfHostedRunnerPresetOSLabels,
			known,
		),
	)

	return compatInvalid
}

func (rule *RuleRunnerLabel) tryToGetLabelsInMatrix(label *String, m *Matrix) []*String {
	if m == nil {
		return nil
	}

	prefix, suffix, prop, ok := parseMatrixLabelTemplate(label.Value)
	if !ok {
		return nil
	}

	labels := []*String{}
	appendLabel := func(v RawYAMLValue) {
		s, ok := v.(*RawYAMLString)
		if !ok {
			return
		}
		value, ok := resolveLiteralInterpolations(s.Value)
		if !ok {
			return
		}
		labels = append(labels, &String{prefix + value + suffix, false, s.Pos()})
	}

	if m.Rows != nil {
		if row, ok := m.Rows[prop]; ok {
			for _, v := range row.Values {
				appendLabel(v)
			}
		}
	}

	if m.Include != nil {
		for _, combi := range m.Include.Combinations {
			if combi.Assigns != nil {
				if assign, ok := combi.Assigns[prop]; ok {
					appendLabel(assign.Value)
				}
			}
		}
	}

	return labels
}

func parseMatrixLabelTemplate(s string) (string, string, string, bool) {
	var prefix, suffix, prop string
	for {
		start := strings.Index(s, "${{")
		if start < 0 {
			if prop == "" {
				prefix += s
			} else {
				suffix += s
			}
			break
		}
		if prop == "" {
			prefix += s[:start]
		} else {
			suffix += s[:start]
		}
		s = s[start+3:]
		end := strings.Index(s, "}}")
		if end < 0 {
			return "", "", "", false
		}
		expr, err := NewExprParser().Parse(NewExprLexer(s[:end] + "}}"))
		if err != nil {
			return "", "", "", false
		}
		switch e := expr.(type) {
		case *StringNode:
			if prop == "" {
				prefix += e.Value
			} else {
				suffix += e.Value
			}
		case *ObjectDerefNode:
			recv, ok := e.Receiver.(*VariableNode)
			if !ok || recv.Name != "matrix" || prop != "" {
				return "", "", "", false
			}
			prop = e.Property
		default:
			return "", "", "", false
		}
		s = s[end+2:]
	}
	return prefix, suffix, prop, prop != ""
}

func resolveLiteralInterpolations(s string) (string, bool) {
	var resolved strings.Builder
	for {
		start := strings.Index(s, "${{")
		if start < 0 {
			resolved.WriteString(s)
			return resolved.String(), true
		}
		resolved.WriteString(s[:start])
		s = s[start+3:]
		end := strings.Index(s, "}}")
		if end < 0 {
			return "", false
		}
		expr, err := NewExprParser().Parse(NewExprLexer(s[:end] + "}}"))
		if err != nil {
			return "", false
		}
		literal, ok := expr.(*StringNode)
		if !ok {
			return "", false
		}
		resolved.WriteString(literal.Value)
		s = s[end+2:]
	}
}

func (rule *RuleRunnerLabel) checkConflict(comp runnerOSCompat, label *String) bool {
	for c, l := range rule.compats {
		if c&comp == 0 {
			rule.Errorf(label.Pos, "label %q conflicts with label %q defined at %s. note: to run your job on each workers, use matrix", label.Value, l.Value, l.Pos)
			return false
		}
	}
	return true
}

func (rule *RuleRunnerLabel) checkCompat(comp runnerOSCompat, label *String) {
	if comp == compatInvalid || !rule.checkConflict(comp, label) {
		return
	}
	if _, ok := rule.compats[comp]; !ok {
		rule.compats[comp] = label
	}
}

func (rule *RuleRunnerLabel) checkCombiCompat(comps []runnerOSCompat, labels []*String) {
	for i, c := range comps {
		if c != compatInvalid && !rule.checkConflict(c, labels[i]) {
			// Overwrite the compatibility value with compatInvalid at conflicted label not to
			// register the label to `rule.compats`.
			comps[i] = compatInvalid
		}
	}
	for i, c := range comps {
		if c != compatInvalid {
			if _, ok := rule.compats[c]; !ok {
				rule.compats[c] = labels[i]
			}
		}
	}
}

func (rule *RuleRunnerLabel) getKnownLabels() []string {
	if rule.config == nil {
		return nil
	}
	return rule.config.SelfHostedRunner.Labels
}
