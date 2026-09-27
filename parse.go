package actionlint

import (
	"bytes"
	"fmt"
	"iter"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// https://pkg.go.dev/go.yaml.in/yaml/v4#Kind
func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias" // Should be unreachable because we resolve all aliases before parsing
	default:
		panic(fmt.Sprintf("unreachable: unknown YAML kind: %v", k))
	}
}

func posAt(n *yaml.Node) *Pos {
	return &Pos{n.Line, n.Column}
}

func newString(n *yaml.Node) *String {
	quoted := n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0
	return &String{n.Value, quoted, posAt(n)}
}

// workflowMappingEntry represents a key-value entry in YAML mapping.
type workflowMappingEntry struct {
	// id is a key in lower case for comparing case-insensitive keys.
	id  string
	key *String
	val *yaml.Node
}

type delayedSprintf struct {
	result string
	// Note: Currently only one string arg is sufficient and it's faster than keeping generic interface{} args.
	// `arg` must not be empty when it is used. Empty value means the argument is unused.
	arg string
}

func sprintf(fmt, arg string) delayedSprintf {
	return delayedSprintf{fmt, arg}
}

func (l *delayedSprintf) String() string {
	if len(l.arg) > 0 {
		// This delayed formatting reduces the number of allocations on parsing workflow by 4.94%
		l.result = fmt.Sprintf(l.result, l.arg)
		l.arg = ""
	}
	return l.result
}

type parser struct {
	errors  []*Error
	ignores map[int][]*regexp.Regexp
	source  []byte
}

func (p *parser) error(n *yaml.Node, m string) {
	p.errors = append(p.errors, &Error{m, "", n.Line, n.Column, "syntax-check"})
}

func (p *parser) errorAt(pos *Pos, m string) {
	p.errors = append(p.errors, &Error{m, "", pos.Line, pos.Col, "syntax-check"})
}

func (p *parser) errorfAt(pos *Pos, format string, args ...interface{}) {
	m := fmt.Sprintf(format, args...)
	p.errorAt(pos, m)
}

func (p *parser) errorf(n *yaml.Node, format string, args ...interface{}) {
	m := fmt.Sprintf(format, args...)
	p.error(n, m)
}

func (p *parser) resolveAliases(root *yaml.Node) {
	type usage struct {
		used    bool
		defined bool
	}

	anchors := map[*yaml.Node]*usage{}

	var resolve func(*yaml.Node) // For recursive call
	resolve = func(n *yaml.Node) {
		var u *usage
		if len(n.Anchor) != 0 {
			u = &usage{}
			anchors[n] = u
		}
		for i, c := range n.Content {
			if c.Kind != yaml.AliasNode {
				resolve(c)
				continue
			}
			// Note: Unknown anchors are detected by go-yaml parser so we don't need to detect them by ourselves.
			if u, ok := anchors[c.Alias]; ok {
				u.used = true
				if u.defined {
					n.Content[i] = c.Alias // Resolved
				} else {
					// Don't resolve the recursive alias because it causes stack overflow on parsing the tree as
					// `RawYAMLValue`. (#610)
					p.errorf(c, "recursive alias %q is found. anchor was declared at line:%d, column:%d", c.Alias.Anchor, c.Alias.Line, c.Alias.Column)
				}
			}
		}
		if u != nil {
			u.defined = true
		}
	}
	resolve(root)

	for n, u := range anchors {
		if !u.used {
			p.errorf(n, "anchor %q is defined but not used", n.Anchor)
		}
	}
}

func (p *parser) unexpectedKey(s *String, sec string, expected []string) {
	if !strings.ContainsRune(sec, ' ') {
		sec = fmt.Sprintf("%q section", sec)
	}
	l := len(expected)
	var m string
	if l == 1 {
		m = fmt.Sprintf("expected %q key for %s but got %q", expected[0], sec, s.Value)
	} else if l > 1 {
		m = fmt.Sprintf("unexpected key %q for %s. expected one of %v", s.Value, sec, sortedQuotes(expected))
	} else {
		m = fmt.Sprintf("unexpected key %q for %s", s.Value, sec)
	}
	p.errorAt(s.Pos, m)
}

func (p *parser) checkNotEmpty(sec string, len int, n *yaml.Node) bool {
	if len == 0 {
		p.errorf(n, "%q section should not be empty", sec)
		return false
	}
	return true
}

func (p *parser) checkSequence(sec string, n *yaml.Node, allowEmpty bool) bool {
	if n.Kind != yaml.SequenceNode {
		p.errorf(n, "%q section must be sequence node but got %s node with %q tag", sec, nodeKindName(n.Kind), n.Tag)
		return false
	}
	return allowEmpty || p.checkNotEmpty(sec, len(n.Content), n)
}

func (p *parser) checkString(n *yaml.Node, allowEmpty bool) bool {
	// Do not check n.Tag is !!str because we don't need to check the node is string strictly.
	// In almost all cases, other nodes (like 42) are handled as string with its string representation.
	if n.Kind != yaml.ScalarNode {
		p.errorf(n, "expected scalar node for string value but found %s node with %q tag", nodeKindName(n.Kind), n.Tag)
		return false
	}
	if !allowEmpty && n.Value == "" {
		p.error(n, "string should not be empty")
		return false
	}
	return true
}

func (p *parser) missingExpression(n *yaml.Node, expecting string) {
	p.errorf(n, "expecting a single ${{...}} expression or %s, but found plain text node", expecting)
}

func (p *parser) parseExpression(n *yaml.Node, expecting string) *String {
	if !isExprAssigned(n.Value) {
		p.missingExpression(n, expecting)
		return nil
	}
	return newString(n)
}

func (p *parser) mayParseExpression(n *yaml.Node) *String {
	if n.Tag != "!!str" {
		return nil
	}
	if !isExprAssigned(n.Value) {
		return nil
	}
	return newString(n)
}

func (p *parser) parseString(n *yaml.Node, allowEmpty bool) *String {
	if !p.checkString(n, allowEmpty) {
		return &String{"", false, posAt(n)}
	}
	return newString(n)
}

func (p *parser) parseStringSequence(sec string, n *yaml.Node, allowEmpty bool, allowElemEmpty bool) []*String {
	if ok := p.checkSequence(sec, n, allowEmpty); !ok {
		return nil
	}

	ss := make([]*String, 0, len(n.Content))
	for _, c := range n.Content {
		s := p.parseString(c, allowElemEmpty)
		if s != nil {
			ss = append(ss, s)
		}
	}
	return ss
}

func (p *parser) parseStringOrStringSequence(sec string, n *yaml.Node, allowEmpty bool, allowElemEmpty bool) []*String {
	switch n.Kind {
	case yaml.ScalarNode:
		if allowEmpty && n.Tag == "!!null" {
			return []*String{} // In the case of 'foo:'
		}
		return []*String{p.parseString(n, allowElemEmpty)}
	default:
		return p.parseStringSequence(sec, n, allowEmpty, allowElemEmpty)
	}
}

func (p *parser) parseBool(n *yaml.Node) *Bool {
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!bool" && n.Tag != "!!str") {
		p.errorf(n, "expected bool value but found %s node with %q tag", nodeKindName(n.Kind), n.Tag)
		return nil
	}

	if n.Tag == "!!str" {
		e := p.parseExpression(n, "boolean literal \"true\" or \"false\"")
		return &Bool{
			Expression: e,
			Pos:        posAt(n),
		}
	}

	return &Bool{
		Value: n.Value == "true",
		Pos:   posAt(n),
	}
}

func (p *parser) parseInt(n *yaml.Node) *Int {
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!int" && n.Tag != "!!str") {
		p.errorf(n, "expected scalar node for integer value but found %s node with %q tag", nodeKindName(n.Kind), n.Tag)
		return nil
	}

	if n.Tag == "!!str" {
		e := p.parseExpression(n, "integer literal")
		if e == nil {
			return nil
		}
		return &Int{
			Expression: e,
			Pos:        posAt(n),
		}
	}

	i, err := strconv.Atoi(n.Value)
	if err != nil {
		p.errorf(n, "invalid integer value: %q: %s", n.Value, err.Error())
		return nil
	}

	return &Int{
		Value: i,
		Pos:   posAt(n),
	}
}

func (p *parser) parseFloat(n *yaml.Node) *Float {
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!float" && n.Tag != "!!int" && n.Tag != "!!str") {
		p.errorf(n, "expected scalar node for float value but found %s node with %q tag", nodeKindName(n.Kind), n.Tag)
		return nil
	}

	if n.Tag == "!!str" {
		e := p.parseExpression(n, "float number literal")
		if e == nil {
			return nil
		}
		return &Float{
			Expression: e,
			Pos:        posAt(n),
		}
	}

	f, err := strconv.ParseFloat(n.Value, 64)
	if err != nil || math.IsNaN(f) {
		p.errorf(n, "invalid float value: %q: %s", n.Value, err.Error())
		return nil
	}

	return &Float{
		Value: f,
		Pos:   posAt(n),
	}
}

func (p *parser) parseMapping(where delayedSprintf, n *yaml.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return func(yield func(workflowMappingEntry) bool) {
		if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
			if !allowEmpty {
				p.errorf(n, "%s should not be empty. please remove this section if it's unnecessary", where.String())
			}
			return
		}

		if n.Kind != yaml.MappingNode {
			p.errorf(n, "%s is %s node but mapping node is expected", where.String(), nodeKindName(n.Kind))
			return
		}

		keys := make(map[string]*Pos, len(n.Content)/2)
		empty := true
		for i := 0; i < len(n.Content); i += 2 {
			k := p.parseString(n.Content[i], false)

			if k.Value == "<<" {
				p.errorAt(k.Pos, "GitHub Actions does not support YAML merge key \"<<\"")
				continue
			}

			id := k.Value
			if !caseSensitive {
				// Keys of mappings are sometimes case insensitive. For example, following matrix is invalid.
				//   matrix:
				//     foo: [1, 2, 3]
				//     FOO: [1, 2, 3]
				// To detect case insensitive duplicate keys, we use lowercase keys
				id = strings.ToLower(id)
			}

			if pos, ok := keys[id]; ok {
				var note string
				if !caseSensitive {
					note = ". note that this key is case insensitive"
				}
				p.errorfAt(k.Pos, "key %q is duplicated in %s. previously defined at %s%s", k.Value, where.String(), pos.String(), note)
				continue
			}

			if !yield(workflowMappingEntry{id, k, n.Content[i+1]}) {
				break
			}

			keys[id] = k.Pos
			empty = false
		}

		if !allowEmpty && empty {
			p.errorf(n, "%s should not be empty. please remove this section if it's unnecessary", where.String())
		}
	}
}

func (p *parser) parseSectionMapping(section string, n *yaml.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf("%q section", section), n, allowEmpty, caseSensitive)
}

func (p *parser) parseMappingAt(where string, n *yaml.Node, allowEmpty, caseSensitive bool) iter.Seq[workflowMappingEntry] {
	return p.parseMapping(sprintf(where, ""), n, allowEmpty, caseSensitive)
}

func (p *parser) parseScheduleEvent(pos *Pos, n *yaml.Node) *ScheduledEvent {
	if ok := p.checkSequence("schedule", n, false); !ok {
		return nil
	}

	schedules := make([]*ScheduleEntry, 0, len(n.Content))
	for _, c := range n.Content {
		entry := &ScheduleEntry{}
		for e := range p.parseMappingAt("element of \"schedule\" section", c, false, true) {
			switch e.id {
			case "cron":
				if s := p.parseString(e.val, false); s.Value != "" {
					entry.Cron = s
				}
			case "timezone":
				if s := p.parseString(e.val, false); s.Value != "" {
					entry.Timezone = s
				}
			default:
				p.unexpectedKey(e.key, "element of \"schedule\" section", []string{"cron", "timezone"})
			}
		}
		if entry.Cron != nil {
			schedules = append(schedules, entry)
		}
	}

	return &ScheduledEvent{schedules, pos}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatchinputs
func (p *parser) parseWorkflowDispatchEventInput(name *String, n *yaml.Node) *DispatchInput {
	ret := &DispatchInput{Name: name}

	for e := range p.parseMappingAt("input settings of workflow_dispatch event", n, true, true) {
		switch e.id {
		case "description":
			ret.Description = p.parseString(e.val, true)
		case "required":
			ret.Required = p.parseBool(e.val)
		case "default":
			ret.Default = p.parseString(e.val, true)
		case "type":
			if !p.checkString(e.val, false) {
				continue
			}
			switch e.val.Value {
			case "string":
				ret.Type = WorkflowDispatchEventInputTypeString
			case "number":
				ret.Type = WorkflowDispatchEventInputTypeNumber
			case "boolean":
				ret.Type = WorkflowDispatchEventInputTypeBoolean
			case "choice":
				ret.Type = WorkflowDispatchEventInputTypeChoice
			case "environment":
				ret.Type = WorkflowDispatchEventInputTypeEnvironment
			default:
				p.errorf(e.val, `input type of workflow_dispatch event must be one of "string", "number", "boolean", "choice", "environment" but got %q`, e.val.Value)
			}
		case "options":
			ret.Options = p.parseStringSequence("options", e.val, false, false)
		default:
			p.unexpectedKey(e.key, "inputs", []string{"description", "required", "default"})
		}
	}

	return ret
}

// - https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
// - https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatch
func (p *parser) parseWorkflowDispatchEvent(pos *Pos, n *yaml.Node) *WorkflowDispatchEvent {
	ret := &WorkflowDispatchEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_dispatch", n, true, true) {
		switch e.id {
		case "if":
			ret.If = p.parseString(e.val, false)
		case "inputs":
			ret.Inputs = map[string]*DispatchInput{}
			for e := range p.parseSectionMapping("inputs", e.val, true, false) {
				ret.Inputs[e.id] = p.parseWorkflowDispatchEventInput(e.key, e.val)
			}
		default:
			p.unexpectedKey(e.key, "workflow_dispatch", []string{"if", "inputs"})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#repository_dispatch
func (p *parser) parseRepositoryDispatchEvent(pos *Pos, n *yaml.Node) *RepositoryDispatchEvent {
	ret := &RepositoryDispatchEvent{Pos: pos}

	// Note: Omitting 'types' is ok. In the case, all types trigger the workflow
	for e := range p.parseSectionMapping("repository_dispatch", n, true, true) {
		switch e.id {
		case "if":
			ret.If = p.parseString(e.val, false)
		case "types":
			ret.Types = p.parseStringOrStringSequence("types", e.val, false, false)
		default:
			p.unexpectedKey(e.key, "repository_dispatch", []string{"if", "types"})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#using-filters
func (p *parser) parseWebhookEventFilter(name *String, n *yaml.Node) *WebhookEventFilter {
	v := p.parseStringOrStringSequence(name.Value, n, false, false)
	return &WebhookEventFilter{name, v}
}

func (p *parser) parseWebhookEvent(name *String, n *yaml.Node) *WebhookEvent {
	ret := &WebhookEvent{Hook: name, Pos: name.Pos}

	// Note: 'tags', 'tags-ignore', 'branches', 'branches-ignore' can be empty. Since there are
	// some cases where setting empty values to them is necessary.
	//
	// > If only define only tags filter (tags/tags-ignore) or only branches filter
	// > (branches/branches-ignore) for on.push, the workflow won’t run for events affecting the
	// > undefined Git ref.
	//
	// https://github.community/t/using-on-push-tags-ignore-and-paths-ignore-together/16931
	for e := range p.parseSectionMapping(name.Value, n, true, true) {
		// Note: Glob pattern cannot be empty, but it is checked by 'glob' rule with better error
		// message. So parser allows empty patterns here.
		switch e.id {
		case "types":
			ret.Types = p.parseStringOrStringSequence(e.key.Value, e.val, false, false)
		case "if":
			ret.If = p.parseString(e.val, false)
		case "branches":
			ret.Branches = p.parseWebhookEventFilter(e.key, e.val)
		case "branches-ignore":
			ret.BranchesIgnore = p.parseWebhookEventFilter(e.key, e.val)
		case "tags":
			ret.Tags = p.parseWebhookEventFilter(e.key, e.val)
		case "tags-ignore":
			ret.TagsIgnore = p.parseWebhookEventFilter(e.key, e.val)
		case "paths":
			ret.Paths = p.parseWebhookEventFilter(e.key, e.val)
		case "paths-ignore":
			ret.PathsIgnore = p.parseWebhookEventFilter(e.key, e.val)
		case "workflows":
			ret.Workflows = p.parseStringOrStringSequence(e.key.Value, e.val, false, false)
		default:
			p.unexpectedKey(e.key, name.Value, []string{
				"types",
				"if",
				"branches",
				"branches-ignore",
				"tags",
				"tags-ignore",
				"paths",
				"paths-ignore",
				"workflows",
			})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onworkflow_callinputs
func (p *parser) parseWorkflowCallEventInput(id string, name *String, n *yaml.Node) *WorkflowCallEventInput {
	ret := &WorkflowCallEventInput{Name: name, ID: id}
	typed := false

	for e := range p.parseMappingAt("input of workflow_call event", n, true, true) {
		switch e.id {
		case "description":
			ret.Description = p.parseString(e.val, true)
		case "required":
			ret.Required = p.parseBool(e.val)
		case "default":
			ret.Default = p.parseString(e.val, true)
		case "type":
			typed = true
			if !p.checkString(e.val, false) {
				continue
			}
			switch e.val.Value {
			case "boolean":
				ret.Type = WorkflowCallEventInputTypeBoolean
			case "number":
				ret.Type = WorkflowCallEventInputTypeNumber
			case "string":
				ret.Type = WorkflowCallEventInputTypeString
			default:
				p.errorf(e.val, "invalid value %q for input type of workflow_call event. it must be one of \"boolean\", \"number\", or \"string\"", e.val.Value)
			}
		default:
			p.unexpectedKey(e.key, "inputs at workflow_call event", []string{"description", "required", "default", "type"})
		}
	}

	if !typed {
		p.errorfAt(name.Pos, "\"type\" is missing at %q input of workflow_call event", name.Value)
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#example-of-onworkflow_callsecrets
func (p *parser) parseWorkflowCallEventSecret(name *String, n *yaml.Node) *WorkflowCallEventSecret {
	ret := &WorkflowCallEventSecret{Name: name}

	for e := range p.parseMappingAt("secret of workflow_call event", n, true, true) {
		switch e.id {
		case "description":
			ret.Description = p.parseString(e.val, true)
		case "required":
			ret.Required = p.parseBool(e.val)
		default:
			p.unexpectedKey(e.key, "secrets", []string{"description", "required"})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#example-of-onworkflow_calloutputs
func (p *parser) parseWorkflowCallEventOutput(name *String, n *yaml.Node) *WorkflowCallEventOutput {
	output := &WorkflowCallEventOutput{Name: name}

	for e := range p.parseMappingAt("output of workflow_call event", n, true, true) {
		switch e.id {
		case "description":
			output.Description = p.parseString(e.val, true)
		case "value":
			output.Value = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "outputs at workflow_call event", []string{"description", "value"})
		}
	}

	if output.Value == nil {
		p.errorfAt(name.Pos, "\"value\" is missing at %q output of workflow_call event", name.Value)
	}

	return output
}

// - https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow-reuse-events
// - https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_call
// - https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
func (p *parser) parseWorkflowCallEvent(pos *Pos, n *yaml.Node) *WorkflowCallEvent {
	ret := &WorkflowCallEvent{Pos: pos}

	for e := range p.parseSectionMapping("workflow_call", n, true, true) {
		switch e.id {
		case "inputs":
			ret.Inputs = []*WorkflowCallEventInput{}
			for e := range p.parseSectionMapping("inputs", e.val, true, false) {
				ret.Inputs = append(ret.Inputs, p.parseWorkflowCallEventInput(e.id, e.key, e.val))
			}
		case "secrets":
			ret.Secrets = map[string]*WorkflowCallEventSecret{}
			for e := range p.parseSectionMapping("secrets", e.val, true, false) {
				ret.Secrets[e.id] = p.parseWorkflowCallEventSecret(e.key, e.val)
			}
		case "outputs":
			ret.Outputs = map[string]*WorkflowCallEventOutput{}
			for e := range p.parseSectionMapping("outputs", e.val, true, false) {
				ret.Outputs[e.id] = p.parseWorkflowCallEventOutput(e.key, e.val)
			}
		default:
			p.unexpectedKey(e.key, "workflow_call", []string{"inputs", "secrets", "outputs"})
		}
	}

	return ret
}

func (p *parser) parseImageVersionEvent(pos *Pos, n *yaml.Node) *ImageVersionEvent {
	ret := &ImageVersionEvent{Pos: pos}

	for e := range p.parseSectionMapping("image_version", n, true, true) {
		switch e.id {
		case "names":
			ret.Names = p.parseStringSequence("names", e.val, false, false)
		case "versions":
			ret.Versions = p.parseStringSequence("versions", e.val, false, false)
		default:
			p.unexpectedKey(e.key, "image_version", []string{"names", "versions"})
		}
	}

	return ret
}

func (p *parser) parseEventWithNoConfig(n *yaml.Node) Event {
	s := p.parseString(n, false)
	switch s.Value {
	case "":
		return nil
	case "schedule":
		p.error(n, "schedule event must be configured with mapping")
		return nil
	case "repository_dispatch":
		return &RepositoryDispatchEvent{Pos: posAt(n)}
	case "workflow_dispatch":
		return &WorkflowDispatchEvent{Pos: posAt(n)}
	case "workflow_call":
		return &WorkflowCallEvent{Pos: posAt(n)}
	case "image_version":
		return &ImageVersionEvent{Pos: posAt(n)}
	default:
		return &WebhookEvent{Hook: s, Pos: posAt(n)}
	}
}

func (p *parser) parseEvents(n *yaml.Node) []Event {
	switch n.Kind {
	case yaml.ScalarNode:
		if e := p.parseEventWithNoConfig(n); e != nil {
			return []Event{e}
		}
		return []Event{}
	case yaml.MappingNode:
		ret := []Event{}
		for e := range p.parseSectionMapping("on", n, false, true) {
			pos := e.key.Pos
			switch e.id {
			case "schedule":
				if e := p.parseScheduleEvent(pos, e.val); e != nil {
					ret = append(ret, e)
				}
			case "workflow_dispatch":
				ret = append(ret, p.parseWorkflowDispatchEvent(pos, e.val))
			case "repository_dispatch":
				ret = append(ret, p.parseRepositoryDispatchEvent(pos, e.val))
			case "workflow_call":
				ret = append(ret, p.parseWorkflowCallEvent(pos, e.val))
			case "image_version":
				ret = append(ret, p.parseImageVersionEvent(pos, e.val))
			default:
				ret = append(ret, p.parseWebhookEvent(e.key, e.val))
			}
		}

		return ret
	case yaml.SequenceNode:
		l := len(n.Content)
		p.checkNotEmpty("on", l, n)
		ret := make([]Event, 0, l)

		for _, c := range n.Content {
			if e := p.parseEventWithNoConfig(c); e != nil {
				ret = append(ret, e)
			}
		}

		return ret
	default:
		p.errorf(n, "\"on\" section value is expected to be mapping or sequence but found %s node", nodeKindName(n.Kind))
		return nil
	}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#permissions
func (p *parser) parsePermissions(pos *Pos, n *yaml.Node) *Permissions {
	ret := &Permissions{Pos: pos}

	if n.Kind == yaml.ScalarNode {
		ret.All = p.parseString(n, false)
	} else {
		// XXX: Is the permission scope case insensitive?
		scopes := map[string]*PermissionScope{}
		for e := range p.parseSectionMapping("permissions", n, true, false) {
			scopes[e.id] = &PermissionScope{
				Name:  e.key,
				Value: p.parseString(e.val, false),
			}
		}
		ret.Scopes = scopes
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#env
func (p *parser) parseEnv(n *yaml.Node) *Env {
	if n.Kind == yaml.ScalarNode {
		return &Env{
			Expression: p.parseExpression(n, "mapping value for \"env\" section"),
		}
	}

	vars := map[string]*EnvVar{}
	for e := range p.parseSectionMapping("env", n, false, false) {
		vars[e.id] = &EnvVar{
			Name:  e.key,
			Value: p.parseString(e.val, true),
		}
	}

	return &Env{Vars: vars}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#defaults
func (p *parser) parseDefaults(pos *Pos, n *yaml.Node) *Defaults {
	ret := &Defaults{Pos: pos}

	for e := range p.parseSectionMapping("defaults", n, false, true) {
		if e.id != "run" {
			p.unexpectedKey(e.key, "defaults", []string{"run"})
			continue
		}
		ret.Run = &DefaultsRun{Pos: e.key.Pos}

		for e := range p.parseSectionMapping("run", e.val, false, true) {
			switch e.id {
			case "shell":
				ret.Run.Shell = p.parseString(e.val, false)
			case "working-directory":
				ret.Run.WorkingDirectory = p.parseString(e.val, false)
			default:
				p.unexpectedKey(e.key, "run", []string{"shell", "working-directory"})
			}
		}
	}

	if ret.Run == nil {
		p.error(n, "\"defaults\" section should have \"run\" section")
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idconcurrency
func (p *parser) parseConcurrency(pos *Pos, n *yaml.Node) *Concurrency {
	ret := &Concurrency{Pos: pos}

	if n.Kind == yaml.ScalarNode {
		ret.Group = p.parseString(n, false)
		return ret
	}

	for e := range p.parseSectionMapping("concurrency", n, false, true) {
		switch e.id {
		case "group":
			ret.Group = p.parseString(e.val, false)
		case "cancel-in-progress":
			ret.CancelInProgress = p.parseBool(e.val)
		case "queue":
			ret.Queue = p.parseString(e.val, false)
			if ret.Queue != nil && !ret.Queue.ContainsExpression() {
				switch ret.Queue.Value {
				case "max", "single":
					// ok
				default:
					p.errorf(e.val, "invalid value %q for \"queue\" in \"concurrency\" section. valid values are \"max\" and \"single\"", ret.Queue.Value)
				}
			}
		default:
			p.unexpectedKey(e.key, "concurrency", []string{"cancel-in-progress", "group", "queue"})
		}
	}
	if ret.Group == nil {
		p.errorAt(pos, "group name is missing in \"concurrency\" section")
	}
	if ret.Queue != nil && ret.CancelInProgress != nil && !ret.Queue.ContainsExpression() && ret.Queue.Value == "max" && ret.CancelInProgress.Expression == nil && ret.CancelInProgress.Value {
		p.errorfAt(ret.Queue.Pos, "\"queue: max\" cannot be combined with \"cancel-in-progress: true\" in \"concurrency\" section")
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idenvironment
func (p *parser) parseEnvironment(pos *Pos, n *yaml.Node) *Environment {
	ret := &Environment{Pos: pos}

	if n.Kind == yaml.ScalarNode {
		ret.Name = p.parseString(n, false)
		return ret
	}

	for e := range p.parseSectionMapping("environment", n, false, true) {
		switch e.id {
		case "name":
			ret.Name = p.parseString(e.val, false)
		case "url":
			ret.URL = p.parseString(e.val, false)
		case "deployment":
			ret.Deployment = p.parseBool(e.val)
		default:
			p.unexpectedKey(e.key, "environment", []string{"deployment", "name", "url"})
		}
	}
	if ret.Name == nil {
		p.errorAt(pos, "name is missing in \"environment\" section")
	}
	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idoutputs
func (p *parser) parseOutputs(n *yaml.Node) map[string]*Output {
	ret := map[string]*Output{}
	for e := range p.parseSectionMapping("outputs", n, false, false) {
		ret[e.id] = &Output{
			Name:  e.key,
			Value: p.parseString(e.val, true),
		}
	}
	p.checkNotEmpty("outputs", len(ret), n)
	return ret
}

func (p *parser) parseRawYAMLValue(n *yaml.Node) RawYAMLValue {
	switch n.Kind {
	case yaml.ScalarNode:
		return &RawYAMLString{n.Value, n.Tag == "!!str", posAt(n)}
	case yaml.SequenceNode:
		vs := make([]RawYAMLValue, 0, len(n.Content))
		for _, c := range n.Content {
			if v := p.parseRawYAMLValue(c); v != nil {
				vs = append(vs, v)
			}
		}
		return &RawYAMLArray{vs, posAt(n)}
	case yaml.MappingNode:
		m := map[string]RawYAMLValue{}
		for e := range p.parseMappingAt("matrix row value", n, true, false) {
			if v := p.parseRawYAMLValue(e.val); v != nil {
				m[e.id] = v
			}
		}
		return &RawYAMLObject{m, posAt(n)}
	default:
		p.errorf(n, "unexpected %s node on parsing value in matrix row", nodeKindName(n.Kind))
		return nil
	}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-including-additional-values-into-combinations
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-excluding-configurations-from-a-matrix
func (p *parser) parseMatrixCombinations(sec string, n *yaml.Node) *MatrixCombinations {
	if n.Kind == yaml.ScalarNode {
		return &MatrixCombinations{
			Expression: p.parseExpression(n, "array of matrix combination"),
		}
	}

	if ok := p.checkSequence(sec, n, false); !ok {
		return nil
	}

	ret := make([]*MatrixCombination, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode {
			if e := p.parseExpression(c, "mapping of matrix combination"); e != nil {
				ret = append(ret, &MatrixCombination{Expression: e})
			}
			continue
		}

		assigns := map[string]*MatrixAssign{}
		for e := range p.parseMapping(sprintf("element in %q section", sec), c, false, false) {
			if v := p.parseRawYAMLValue(e.val); v != nil {
				assigns[e.id] = &MatrixAssign{e.key, v}
			}
		}
		ret = append(ret, &MatrixCombination{Assigns: assigns})
	}
	return &MatrixCombinations{Combinations: ret}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
func (p *parser) parseMatrix(pos *Pos, n *yaml.Node) *Matrix {
	if n.Kind == yaml.ScalarNode {
		return &Matrix{
			Expression: p.parseExpression(n, "matrix"),
			Pos:        posAt(n),
		}
	}

	ret := &Matrix{Pos: pos, Rows: make(map[string]*MatrixRow)}

	for e := range p.parseSectionMapping("matrix", n, false, false) {
		switch e.id {
		case "include":
			ret.Include = p.parseMatrixCombinations("include", e.val)
		case "exclude":
			ret.Exclude = p.parseMatrixCombinations("exclude", e.val)
		default:
			if e.val.Kind == yaml.ScalarNode {
				ret.Rows[e.id] = &MatrixRow{
					Expression: p.parseExpression(e.val, "array value for matrix variations"),
				}
				continue
			}

			if ok := p.checkSequence("matrix values", e.val, false); !ok {
				continue
			}

			values := make([]RawYAMLValue, 0, len(e.val.Content))
			for _, c := range e.val.Content {
				if v := p.parseRawYAMLValue(c); v != nil {
					values = append(values, v)
				}
			}

			ret.Rows[e.id] = &MatrixRow{
				Name:   e.key,
				Values: values,
			}
		}
	}

	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idstrategymax-parallel
func (p *parser) parseMaxParallel(n *yaml.Node) *Int {
	i := p.parseInt(n)
	if i != nil && i.Expression == nil && i.Value <= 0 {
		p.errorf(n, "value at \"max-parallel\" must be greater than zero: %v", i.Value)
	}
	return i
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategy
func (p *parser) parseStrategy(pos *Pos, n *yaml.Node) *Strategy {
	ret := &Strategy{Pos: pos}

	for e := range p.parseSectionMapping("strategy", n, false, true) {
		switch e.id {
		case "matrix":
			ret.Matrix = p.parseMatrix(e.key.Pos, e.val)
		case "fail-fast":
			ret.FailFast = p.parseBool(e.val)
		case "max-parallel":
			ret.MaxParallel = p.parseMaxParallel(e.val)
		default:
			p.unexpectedKey(e.key, "strategy", []string{"matrix", "fail-fast", "max-parallel"})
		}
	}

	return ret
}

func (p *parser) parseCredentials(pos *Pos, n *yaml.Node) *Credentials {
	ret := &Credentials{Pos: pos}

	if e := p.mayParseExpression(n); e != nil {
		ret.Expression = e
		return ret
	}

	for e := range p.parseSectionMapping("credentials", n, false, true) {
		switch e.id {
		case "username":
			ret.Username = p.parseString(e.val, false)
		case "password":
			ret.Password = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "credentials", []string{"username", "password"})
		}
	}

	if ret.Username == nil || ret.Password == nil {
		p.errorAt(pos, "both \"username\" and \"password\" must be specified in \"credentials\" section")
		return nil
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idcontainer
func (p *parser) parseContainer(sec string, pos *Pos, n *yaml.Node) *Container {
	ret := &Container{Pos: pos}
	keys := []string{
		"image",
		"credentials",
		"env",
		"ports",
		"volumes",
		"options",
	}
	if sec == "services" {
		keys = append(keys, "command", "entrypoint")
	}

	if n.Kind == yaml.ScalarNode {
		// When you only specify a container image, you can omit the image keyword.
		ret.Image = p.parseString(n, false)
		return ret
	}

	for e := range p.parseSectionMapping(sec, n, false, true) {
		switch e.id {
		case "image":
			ret.Image = p.parseString(e.val, false)
		case "credentials":
			ret.Credentials = p.parseCredentials(e.key.Pos, e.val)
		case "env":
			ret.Env = p.parseEnv(e.val)
		case "ports":
			ret.Ports = p.parseStringSequence("ports", e.val, true, false)
		case "volumes":
			ret.Ports = p.parseStringSequence("volumes", e.val, true, false)
		case "options":
			ret.Options = p.parseString(e.val, true)
		case "command":
			if sec == "services" {
				ret.Command = p.parseString(e.val, true)
				continue
			}
			p.unexpectedKey(e.key, sec, keys)
		case "entrypoint":
			if sec == "services" {
				ret.Entrypoint = p.parseString(e.val, true)
				continue
			}
			p.unexpectedKey(e.key, sec, keys)
		default:
			p.unexpectedKey(e.key, sec, keys)
		}
	}

	if ret.Image == nil {
		p.errorfAt(pos, "\"image\" is missing in %q section", sec)
	}

	return ret
}

func (p *parser) parseServices(n *yaml.Node) *Services {
	ret := &Services{Pos: posAt(n)}
	if e := p.mayParseExpression(n); e != nil {
		ret.Expression = e
	} else {
		// XXX: Is the key case-insensitive?
		ss := map[string]*Service{}
		for e := range p.parseSectionMapping("services", n, false, false) {
			ss[e.id] = &Service{
				Name:      e.key,
				Container: p.parseContainer("services", e.key.Pos, e.val),
			}
		}
		ret.Value = ss
	}
	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
func (p *parser) parseTimeoutMinutes(n *yaml.Node) *Float {
	f := p.parseFloat(n)
	if f != nil && f.Expression == nil && f.Value <= 0.0 {
		p.errorf(n, "value at \"timeout-minutes\" must be greater than zero: %v", f.Value)
	}
	return f
}

func (p *parser) parseStepExecAction(entries []workflowMappingEntry, isDocker bool) *ExecAction {
	ret := &ExecAction{}

	for _, e := range entries {
		switch e.id {
		case "uses":
			ret.Uses = p.parseString(e.val, false)
		case "with":
			ret.Inputs = map[string]*Input{}
			with := p.parseSectionMapping("with", e.val, false, false)
			if isDocker {
				for e := range with {
					switch e.id {
					case "entrypoint":
						// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepswithentrypoint
						ret.Entrypoint = p.parseString(e.val, false)
					case "args":
						// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepswithargs
						ret.Args = p.parseString(e.val, true)
					default:
						ret.Inputs[e.id] = &Input{e.key, p.parseString(e.val, true)}
					}
				}
			} else {
				for e := range with {
					ret.Inputs[e.id] = &Input{e.key, p.parseString(e.val, true)}
				}
			}
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes", "background":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to execute action", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"background",
				"uses",
				"with",
			})
		}
	}

	// Note: `ret.Uses` is never `nil` because `parseStep` checks `uses` key in advance
	return ret
}

// plainRunSpansMultipleLines examines physical source lines only for plain run
// scalars starting below their key. YAML folds such lines into one command.
func (p *parser) plainRunSpansMultipleLines(value *yaml.Node) bool {
	line, offset := 1, 0
	for line < value.Line {
		next := bytes.IndexByte(p.source[offset:], '\n')
		if next < 0 {
			return false
		}
		offset += next + 1
		line++
	}
	count := 0
	for offset < len(p.source) {
		next := bytes.IndexByte(p.source[offset:], '\n')
		end := len(p.source)
		if next >= 0 {
			end = offset + next
		}
		text := p.source[offset:end]
		indent := 0
		for indent < len(text) && text[indent] == ' ' {
			indent++
		}
		if indent < len(text) && text[indent] != '#' {
			if indent+1 < value.Column {
				break
			}
			count++
			if count > 1 {
				return true
			}
		}
		offset = end + 1
	}
	return false
}

func (p *parser) parseStepExecRun(entries []workflowMappingEntry) *ExecRun {
	ret := &ExecRun{}

	for _, e := range entries {
		switch e.id {
		case "run":
			ret.Run = p.parseString(e.val, false)
			ret.RunPos = e.key.Pos
			if e.val.Style == 0 && e.val.Line > e.key.Pos.Line && p.plainRunSpansMultipleLines(e.val) {
				p.errorfAt(e.key.Pos, "multi-line \"run\" script must use a literal block scalar (run: |) instead of folding commands into one line")
			}
		case "shell":
			ret.Shell = p.parseString(e.val, false)
		case "working-directory":
			ret.WorkingDirectory = p.parseString(e.val, false)
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes", "background":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to run shell command", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"background",
				"run",
				"shell",
				"working-directory",
			})
		}
	}

	// Note: `ret.Run` is never `nil` because `parseStep` checks `run` key in advance
	return ret
}

// parseStepExecWait parses a 'wait' or 'wait-all' step that waits for background steps to complete.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *parser) parseStepExecWait(entries []workflowMappingEntry) *ExecWait {
	ret := &ExecWait{}
	waitGiven := false

	for _, e := range entries {
		switch e.id {
		case "wait":
			waitGiven = true
			ret.Names = p.parseStringOrStringSequence("wait", e.val, false, false)
		case "wait-all":
			// 'wait-all' waits for all background steps and takes no arguments.
			if e.val.Tag != "!!null" {
				p.error(e.val, "\"wait-all\" step takes no arguments")
			}
			ret.All = true
			ret.AllPos = e.key.Pos
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to wait for background steps", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"wait",
				"wait-all",
			})
		}
	}

	// 'wait' and 'wait-all' are mutually exclusive: 'wait' targets specific
	// background steps while 'wait-all' waits for all of them.
	if waitGiven && ret.All {
		p.errorAt(ret.AllPos, "\"wait\" and \"wait-all\" cannot be specified in the same step")
	}

	return ret
}

// parseStepExecCancel parses a 'cancel' step that cancels running background steps.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *parser) parseStepExecCancel(entries []workflowMappingEntry) *ExecCancel {
	ret := &ExecCancel{}

	for _, e := range entries {
		switch e.id {
		case "cancel":
			// The 'cancel' step targets a single background step by its ID (not a list).
			ret.Name = p.parseString(e.val, false)
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to cancel background steps", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"cancel",
			})
		}
	}

	return ret
}

// parseStepExecParallel parses a 'parallel' step that runs a group of steps in parallel.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
func (p *parser) parseStepExecParallel(entries []workflowMappingEntry, requireShell bool) *ExecParallel {
	ret := &ExecParallel{}

	for _, e := range entries {
		switch e.id {
		case "parallel":
			ret.Steps = p.parseSteps(e.val, requireShell)
		case "id", "if", "name", "env", "continue-on-error", "timeout-minutes":
			// do nothing
		default:
			p.unexpectedKey(e.key, "step to run steps in parallel", []string{
				"id",
				"if",
				"name",
				"env",
				"continue-on-error",
				"timeout-minutes",
				"parallel",
			})
		}
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idsteps
func (p *parser) parseStep(n *yaml.Node, requireShell bool) *Step {
	ret := &Step{Pos: posAt(n)}

	const (
		isUnknown = iota
		isAction
		isDocker
		isRun
		isWait
		isCancel
		isParallel
	)

	kind := isUnknown
	entries := slices.Collect(p.parseMappingAt("element of \"steps\" section", n, false, true))
	for _, e := range entries {
		switch e.id {
		case "id":
			ret.ID = p.parseString(e.val, false)
		case "if":
			ret.If = p.parseString(e.val, false)
		case "name":
			ret.Name = p.parseString(e.val, true)
		case "env":
			ret.Env = p.parseEnv(e.val)
		case "continue-on-error":
			ret.ContinueOnError = p.parseBool(e.val)
		case "timeout-minutes":
			ret.TimeoutMinutes = p.parseTimeoutMinutes(e.val)
			if requireShell {
				p.errorAt(e.key.Pos, "\"timeout-minutes\" is not available in steps of composite actions")
			}
		case "background":
			ret.Background = p.parseBool(e.val)
		case "uses":
			if strings.HasPrefix(e.val.Value, "docker://") {
				kind = isDocker
			} else {
				kind = isAction
			}
		case "run":
			kind = isRun
			// Note: Unexpected keys are checked in parseStepExecAction or parseStepExecRun later
		case "wait", "wait-all":
			kind = isWait
		case "cancel":
			kind = isCancel
		case "parallel":
			kind = isParallel
			// Note: Unexpected keys are checked in the parseStepExec* functions later
		}
	}

	switch kind {
	case isAction, isDocker:
		ret.Exec = p.parseStepExecAction(entries, kind == isDocker)
	case isRun:
		ret.Exec = p.parseStepExecRun(entries)
		if requireShell && ret.Exec.(*ExecRun).Shell == nil {
			p.error(n, "\"shell\" is required to run script in step as part of composite actions")
		}
	case isWait:
		ret.Exec = p.parseStepExecWait(entries)
	case isCancel:
		ret.Exec = p.parseStepExecCancel(entries)
	case isParallel:
		ret.Exec = p.parseStepExecParallel(entries, requireShell)
	default:
		p.error(n, "step must run script with \"run\" section or run action with \"uses\" section")
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idsteps
func (p *parser) parseSteps(n *yaml.Node, requireShell bool) []*Step {
	if ok := p.checkSequence("steps", n, false); !ok {
		return nil
	}

	ret := make([]*Step, 0, len(n.Content))

	for _, c := range n.Content {
		if s := p.parseStep(c, requireShell); s != nil {
			ret = append(ret, s)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idruns-on
func (p *parser) parseRunsOn(n *yaml.Node) *Runner {
	if expr := p.mayParseExpression(n); expr != nil {
		return &Runner{nil, expr, nil}
	}

	if n.Kind == yaml.ScalarNode || n.Kind == yaml.SequenceNode {
		labels := p.parseStringOrStringSequence("runs-on", n, false, false)
		return &Runner{labels, nil, nil}
	}

	r := &Runner{}
	for e := range p.parseSectionMapping("runs-on", n, false, true) {
		switch e.id {
		case "labels":
			if expr := p.mayParseExpression(e.val); expr != nil {
				r.LabelsExpr = expr
				continue
			}
			r.Labels = p.parseStringOrStringSequence("labels", e.val, false, false)
		case "group":
			r.Group = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "runs-on", []string{"labels", "group"})
		}
	}

	return r
}

func (p *parser) parseSnapshot(pos *Pos, n *yaml.Node) *Snapshot {
	switch n.Kind {
	case yaml.ScalarNode:
		return &Snapshot{ImageName: p.parseString(n, false)}
	case yaml.MappingNode:
		ret := &Snapshot{}
		for e := range p.parseSectionMapping("on", n, false, true) {
			switch e.id {
			case "image-name":
				ret.ImageName = p.parseString(e.val, false)
			case "version":
				ret.Version = p.parseString(e.val, false)
			case "if":
				ret.If = p.parseString(e.val, false)
			default:
				p.unexpectedKey(e.key, "snapshot", []string{"image-name", "version", "if"})
			}
		}
		if ret.ImageName == nil {
			p.errorAt(pos, "\"snapshot\" section must have \"image-name\" configuration")
		}
		return ret
	default:
		p.errorf(n, "\"snapshot\" section value must be string or mapping but found %s node", nodeKindName(n.Kind))
		return nil
	}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#cache-mode
func (p *parser) parseCacheMode(n *yaml.Node) *String {
	if !p.checkString(n, false) {
		return nil
	}
	switch n.Value {
	case "read", "write", "write-only", "none":
		return newString(n)
	default:
		p.errorf(n, "invalid cache-mode %q. expected \"read\", \"write\", \"write-only\", or \"none\"", n.Value)
		return nil
	}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_id
func (p *parser) parseJob(id *String, n *yaml.Node) *Job {
	ret := &Job{ID: id, Pos: id.Pos}
	call := &WorkflowCall{}

	// Only below keys are allowed on reusable workflow call
	// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows#supported-keywords-for-jobs-that-call-a-reusable-workflow
	//   - jobs.<job_id>.name
	//   - jobs.<job_id>.uses
	//   - jobs.<job_id>.with
	//   - jobs.<job_id>.with.<input_id>
	//   - jobs.<job_id>.secrets
	//   - jobs.<job_id>.secrets.<secret_id>
	//   - jobs.<job_id>.needs
	//   - jobs.<job_id>.if
	//   - jobs.<job_id>.permissions
	//   - jobs.<job_id>.cache-mode

	// https://docs.github.com/en/actions/using-workflows/reusing-workflows#supported-keywords-for-jobs-that-call-a-reusable-workflow
	var stepsOnlyKey *String
	var callOnlyKey *String

	for e := range p.parseMapping(sprintf("%q job", id.Value), n, false, true) {
		k, v := e.key, e.val
		switch e.id {
		case "name":
			ret.Name = p.parseString(v, true)
		case "needs":
			if v.Kind == yaml.ScalarNode {
				// needs: job1
				ret.Needs = []*String{p.parseString(v, false)}
			} else {
				// needs: [job1, job2]
				ret.Needs = p.parseStringSequence("needs", v, false, false)
			}
		case "runs-on":
			ret.RunsOn = p.parseRunsOn(v)
			stepsOnlyKey = k
		case "permissions":
			ret.Permissions = p.parsePermissions(k.Pos, v)
		case "cache-mode":
			ret.CacheMode = p.parseCacheMode(v)
		case "environment":
			ret.Environment = p.parseEnvironment(k.Pos, v)
			stepsOnlyKey = k
		case "concurrency":
			ret.Concurrency = p.parseConcurrency(k.Pos, v)
		case "outputs":
			ret.Outputs = p.parseOutputs(v)
			stepsOnlyKey = k
		case "env":
			ret.Env = p.parseEnv(v)
			stepsOnlyKey = k
		case "defaults":
			ret.Defaults = p.parseDefaults(k.Pos, v)
			stepsOnlyKey = k
		case "if":
			ret.If = p.parseString(v, false)
		case "steps":
			ret.Steps = p.parseSteps(v, false)
			stepsOnlyKey = k
		case "timeout-minutes":
			ret.TimeoutMinutes = p.parseTimeoutMinutes(v)
			stepsOnlyKey = k
		case "strategy":
			ret.Strategy = p.parseStrategy(k.Pos, v)
		case "continue-on-error":
			ret.ContinueOnError = p.parseBool(v)
			stepsOnlyKey = k
		case "container":
			ret.Container = p.parseContainer("container", k.Pos, v)
			stepsOnlyKey = k
		case "services":
			ret.Services = p.parseServices(v)
		case "uses":
			call.Uses = p.parseString(v, false)
			callOnlyKey = k
		case "with":
			call.Inputs = map[string]*WorkflowCallInput{}
			for e := range p.parseSectionMapping("with", v, false, false) {
				call.Inputs[e.id] = &WorkflowCallInput{
					Name:  e.key,
					Value: p.parseString(e.val, true),
				}
			}
			callOnlyKey = k
		case "secrets":
			if e.val.Kind == yaml.ScalarNode {
				// `secrets: inherit` special case
				// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#onworkflow_callsecretsinherit
				if e.val.Value == "inherit" {
					call.InheritSecrets = true
				} else {
					p.errorf(e.val, "expected mapping node for secrets or \"inherit\" string node but found %q node", e.val.Value)
				}
			} else {
				call.Secrets = map[string]*WorkflowCallSecret{}
				for e := range p.parseSectionMapping("secrets", v, false, false) {
					call.Secrets[e.id] = &WorkflowCallSecret{
						Name:  e.key,
						Value: p.parseString(e.val, true),
					}
				}
			}
			callOnlyKey = k
		case "snapshot":
			ret.Snapshot = p.parseSnapshot(k.Pos, v)
		default:
			p.unexpectedKey(e.key, "job", []string{
				"name",
				"needs",
				"runs-on",
				"permissions",
				"cache-mode",
				"environment",
				"concurrency",
				"outputs",
				"env",
				"defaults",
				"if",
				"steps",
				"timeout-minutes",
				"strategy",
				"continue-on-error",
				"container",
				"services",
				"uses",
				"with",
				"secrets",
				"snapshot",
			})
		}
	}

	if call.Uses != nil {
		if stepsOnlyKey != nil {
			p.errorfAt(
				stepsOnlyKey.Pos,
				"when a reusable workflow is called with \"uses\", %q is not available. only following keys are allowed: \"name\", \"uses\", \"with\", \"secrets\", \"needs\", \"if\", \"permissions\", and \"cache-mode\" in job %q",
				stepsOnlyKey.Value,
				id.Value,
			)
		} else {
			ret.WorkflowCall = call
		}
	} else {
		// When not a reusable call
		if ret.Steps == nil {
			p.errorfAt(id.Pos, "\"steps\" section is missing in job %q", id.Value)
		}
		if ret.RunsOn == nil {
			p.errorfAt(id.Pos, "\"runs-on\" section is missing in job %q", id.Value)
		}
		if callOnlyKey != nil {
			p.errorfAt(
				callOnlyKey.Pos,
				"%q is only available for a reusable workflow call with \"uses\" but \"uses\" is not found in job %q",
				callOnlyKey.Value,
				id.Value,
			)
		}
	}

	return ret
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobs
func (p *parser) parseJobs(n *yaml.Node) map[string]*Job {
	ret := map[string]*Job{}
	for e := range p.parseSectionMapping("jobs", n, false, false) {
		ret[e.id] = p.parseJob(e.key, e.val)
	}
	return ret
}

func (p *parser) parseActionInput(name *String, n *yaml.Node) *ActionInput {
	input := &ActionInput{Name: name}
	for e := range p.parseMappingAt("input definition", n, false, true) {
		switch e.id {
		case "description":
			input.Description = p.parseString(e.val, false)
		case "required":
			input.Required = p.parseBool(e.val)
		case "default":
			input.Default = p.parseString(e.val, true)
		case "deprecationMessage":
			input.DeprecationMessage = p.parseString(e.val, true)
		default:
			p.unexpectedKey(e.key, "input definition", []string{"description", "required", "default", "deprecationMessage"})
		}
	}
	if input.Description == nil {
		p.errorfAt(name.Pos, "\"description\" is required for input %q", name.Value)
	}
	return input
}

func (p *parser) parseActionOutputs(n *yaml.Node) map[string]*ActionOutput {
	outputs := map[string]*ActionOutput{}
	for e := range p.parseSectionMapping("outputs", n, false, false) {
		output := &ActionOutput{Name: e.key}
		for e := range p.parseMappingAt("output definition", e.val, false, true) {
			switch e.id {
			case "description":
				output.Description = p.parseString(e.val, false)
			case "value":
				output.Value = p.parseString(e.val, false)
			default:
				p.unexpectedKey(e.key, "output definition", []string{"description", "value"})
			}
		}
		outputs[e.id] = output
	}
	return outputs
}

func (p *parser) parseActionRuns(n *yaml.Node) ActionRuns {
	entries := slices.Collect(p.parseSectionMapping("runs", n, false, true))
	var using *String
	for _, e := range entries {
		if e.id == "using" {
			using = p.parseString(e.val, false)
			break
		}
	}
	if using == nil {
		p.error(n, "\"using\" is required in \"runs\" section")
		return nil
	}

	switch using.Value {
	case "node20", "node24":
		runs := &JavaScriptActionRuns{Using: using}
		for _, e := range entries {
			switch e.id {
			case "using":
			case "main":
				runs.Main = p.parseString(e.val, false)
			case "pre":
				runs.Pre = p.parseString(e.val, false)
			case "pre-if":
				runs.PreIf = p.parseString(e.val, false)
			case "post":
				runs.Post = p.parseString(e.val, false)
			case "post-if":
				runs.PostIf = p.parseString(e.val, false)
			default:
				p.unexpectedKey(e.key, "runs for JavaScript action", []string{"using", "main", "pre", "pre-if", "post", "post-if"})
			}
		}
		if runs.Main == nil {
			p.errorAt(using.Pos, "\"main\" is required in \"runs\" section for JavaScript actions")
		}
		return runs
	case "docker":
		runs := &DockerActionRuns{}
		for _, e := range entries {
			switch e.id {
			case "using":
			case "image":
				runs.Image = p.parseString(e.val, false)
			case "pre-entrypoint":
				runs.PreEntrypoint = p.parseString(e.val, false)
			case "entrypoint":
				runs.Entrypoint = p.parseString(e.val, false)
			case "post-entrypoint":
				runs.PostEntrypoint = p.parseString(e.val, false)
			case "args":
				runs.Args = p.parseStringSequence("args", e.val, true, false)
			case "env":
				runs.Env = p.parseEnv(e.val)
			default:
				p.unexpectedKey(e.key, "runs for Docker action", []string{"using", "image", "pre-entrypoint", "entrypoint", "post-entrypoint", "args", "env"})
			}
		}
		if runs.Image == nil {
			p.errorAt(using.Pos, "\"image\" is required in \"runs\" section for Docker actions")
		}
		return runs
	case "composite":
		runs := &CompositeActionRuns{}
		for _, e := range entries {
			switch e.id {
			case "using":
			case "steps":
				runs.Steps = p.parseSteps(e.val, true)
			default:
				p.unexpectedKey(e.key, "runs for composite action", []string{"using", "steps"})
			}
		}
		if runs.Steps == nil {
			p.errorAt(using.Pos, "\"steps\" is required in \"runs\" section for composite actions")
		}
		return runs
	default:
		p.errorfAt(using.Pos, "invalid runner name %q at \"runs.using\". valid runners are \"composite\", \"docker\", \"node20\", and \"node24\"", using.Value)
		return nil
	}
}

func (p *parser) parseActionBranding(n *yaml.Node) *ActionBranding {
	branding := &ActionBranding{}
	for e := range p.parseSectionMapping("branding", n, false, true) {
		switch e.id {
		case "icon":
			branding.Icon = p.parseString(e.val, false)
		case "color":
			branding.Color = p.parseString(e.val, false)
		default:
			p.unexpectedKey(e.key, "branding", []string{"icon", "color"})
		}
	}
	return branding
}

func (p *parser) parseAction(n *yaml.Node) *Action {
	action := &Action{}
	if n.Line == 0 {
		n.Line = 1
	}
	if n.Column == 0 {
		n.Column = 1
	}
	if len(n.Content) == 0 {
		p.error(n, "action metadata is empty")
		return action
	}

	for e := range p.parseSectionMapping("action metadata", n.Content[0], false, true) {
		switch e.id {
		case "name":
			action.Name = p.parseString(e.val, false)
		case "description":
			action.Description = p.parseString(e.val, false)
		case "author":
			action.Author = p.parseString(e.val, false)
		case "inputs":
			action.Inputs = map[string]*ActionInput{}
			for e := range p.parseSectionMapping("inputs", e.val, false, false) {
				action.Inputs[e.id] = p.parseActionInput(e.key, e.val)
			}
		case "outputs":
			action.Outputs = p.parseActionOutputs(e.val)
		case "runs":
			action.Runs = p.parseActionRuns(e.val)
		case "branding":
			action.Branding = p.parseActionBranding(e.val)
		default:
			p.unexpectedKey(e.key, "action metadata", []string{"name", "description", "author", "inputs", "outputs", "runs", "branding"})
		}
	}

	if action.Name == nil {
		p.error(n, "\"name\" is required in action metadata")
	}
	if action.Description == nil {
		p.error(n, "\"description\" is required in action metadata")
	}
	if action.Runs == nil {
		p.error(n, "\"runs\" is required in action metadata")
	}
	if _, ok := action.Runs.(*CompositeActionRuns); ok {
		for _, output := range action.Outputs {
			if output.Value == nil {
				p.errorAt(output.Name.Pos, "\"value\" is required for outputs of composite actions")
			}
		}
	} else if action.Runs != nil {
		for _, output := range action.Outputs {
			if output.Value != nil {
				p.errorAt(output.Value.Pos, "\"value\" is only allowed for outputs of composite actions")
			}
		}
	}
	return action
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions
func (p *parser) parseWorkflow(n *yaml.Node) *Workflow {

	w := &Workflow{}

	if n.Line == 0 {
		n.Line = 1
	}
	if n.Column == 0 {
		n.Column = 1
	}

	if len(n.Content) == 0 {
		p.error(n, "workflow is empty")
		return w
	}

	for e := range p.parseSectionMapping("workflow", n.Content[0], false, true) {
		k, v := e.key, e.val
		switch e.id {
		case "name":
			w.Name = p.parseString(v, true)
		case "on":
			w.On = p.parseEvents(v)
		case "permissions":
			w.Permissions = p.parsePermissions(k.Pos, v)
		case "cache-mode":
			w.CacheMode = p.parseCacheMode(v)
		case "env":
			w.Env = p.parseEnv(v)
		case "defaults":
			w.Defaults = p.parseDefaults(k.Pos, v)
		case "concurrency":
			w.Concurrency = p.parseConcurrency(k.Pos, v)
		case "jobs":
			w.Jobs = p.parseJobs(v)
		case "run-name":
			w.RunName = p.parseString(v, false)
		default:
			p.unexpectedKey(k, "workflow", []string{
				"name",
				"run-name",
				"on",
				"permissions",
				"cache-mode",
				"env",
				"defaults",
				"concurrency",
				"jobs",
			})
		}
	}

	if w.On == nil {
		p.error(n, "\"on\" section is missing in workflow")
	}
	if w.Jobs == nil {
		p.error(n, "\"jobs\" section is missing in workflow")
	}

	return w
}

// func dumpYAML(n *yaml.Node, level int) {
// 	fmt.Printf("%s%s (%s, %d,%d): %q\n", strings.Repeat(". ", level), nodeKindName(n.Kind), n.Tag, n.Line, n.Column, n.Value)
// 	for _, c := range n.Content {
// 		dumpYAML(c, level+1)
// 	}
// }

func handleYAMLUnmarshalError(err error) []*Error {
	if te, ok := err.(*yaml.TypeError); ok {
		errs := make([]*Error, 0, len(te.Errors))
		for _, e := range te.Errors {
			errs = append(errs, &Error{
				Message: fmt.Sprintf("could not parse as YAML: %s", e.Err.Error()),
				Line:    e.Line,
				Column:  e.Column,
				Kind:    "syntax-check",
			})
		}
		return errs
	}

	var m string
	var l int
	var c int
	if pe, ok := err.(*yaml.ParserError); ok {
		l = pe.Line
		c = pe.Column
		m = pe.Message
	} else {
		m = err.Error() // Fallback. I believe this line should be unreachable
	}
	return []*Error{&Error{
		Message: fmt.Sprintf("could not parse as YAML: %s", m),
		Kind:    "syntax-check",
		Line:    l,
		Column:  c,
	}}
}

// InputFormat controls which GitHub Actions YAML syntax is parsed.
type InputFormat uint8

const (
	// FileAutoDetect chooses action metadata for action.yml/action.yaml and workflows otherwise.
	FileAutoDetect InputFormat = iota
	// FileWorkflow parses input as a workflow.
	FileWorkflow
	// FileAction parses input as an action metadata file.
	FileAction
)

func selectInputFormat(path string, format InputFormat) InputFormat {
	if format != FileAutoDetect {
		return format
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.Contains(path, "/.github/workflows/") || strings.HasPrefix(path, ".github/workflows/") {
		return FileWorkflow
	}
	if strings.HasSuffix(path, "/action.yml") || strings.HasSuffix(path, "/action.yaml") || path == "action.yml" || path == "action.yaml" {
		return FileAction
	}
	return FileWorkflow
}

// collectInlineIgnores associates a comment immediately before a YAML node with
// diagnostics reported at that node's line. Block scalar contents are not YAML
// comments, so directives in scripts cannot suppress unrelated diagnostics.
func (p *parser) collectInlineIgnores(node *yaml.Node) {
	if strings.Contains(node.HeadComment, "yactionlint ignore=") {
		for _, line := range strings.Split(node.HeadComment, "\n") {
			line = strings.TrimSpace(line)
			const prefix = "# yactionlint ignore="
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			pattern := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if pattern == "" {
				p.error(node, "inline ignore pattern must not be empty")
				continue
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				p.errorf(node, "invalid inline ignore pattern %q: %v", pattern, err)
				continue
			}
			if p.ignores == nil {
				p.ignores = make(map[int][]*regexp.Regexp)
			}
			p.ignores[node.Line] = append(p.ignores[node.Line], re)
		}
	}
	for _, child := range node.Content {
		p.collectInlineIgnores(child)
	}
}

// ParseFile parses a workflow or action metadata file. It returns its selected input format together
// with any syntax errors found while parsing.
func ParseFile(path string, b []byte, format InputFormat) (*Workflow, *Action, InputFormat, []*Error) {
	w, a, selected, _, errs := parseFile(path, b, format)
	return w, a, selected, errs
}

func parseFile(path string, b []byte, format InputFormat) (*Workflow, *Action, InputFormat, map[int][]*regexp.Regexp, []*Error) {
	var n yaml.Node
	if err := yaml.Unmarshal(b, &n); err != nil {
		return nil, nil, format, nil, handleYAMLUnmarshalError(err)
	}

	p := &parser{source: b}
	p.resolveAliases(&n)
	p.collectInlineIgnores(&n)
	format = selectInputFormat(path, format)
	if format == FileAction {
		a := p.parseAction(&n)
		return nil, a, format, p.ignores, p.errors
	}
	w := p.parseWorkflow(&n)
	return w, nil, format, p.ignores, p.errors
}

// Parse parses input as a workflow. It is kept for API compatibility.
func Parse(b []byte) (*Workflow, []*Error) {
	w, _, _, errs := ParseFile("<stdin>", b, FileWorkflow)
	return w, errs
}
