// Package communication resolves and applies the worker_messages policy:
// which worker may send which message type to which participant, inside an
// active assignment. It is the single policy engine in Go: the CLI and the
// plugin call it and must not keep a second copy (and no second engine in
// JavaScript).
package communication

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Mode values of the worker_messages key.
const (
	ModeOff    = "off"
	ModePolicy = "policy"
)

const (
	modeKey    = "worker_messages"
	rulePrefix = "worker_messages_rules_"
	// EnvRulePrefix is the environment prefix of the highest rule layer:
	// HERDR_SOHO_WORKER_MESSAGES_RULES_<NAME>_{FROM,TO,TYPES,SCOPE,ENABLED}.
	EnvRulePrefix   = "HERDR_SOHO_WORKER_MESSAGES_RULES_"
	scopeAssignment = "assignment"
)

// MessageTypes are the message types the first version allows a rule to
// list; a new type is added here, never by a new rule mechanism.
var MessageTypes = []string{"review.ready", "review.question", "review.finding", "review.result"}

var (
	ruleNameRE    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	ruleKeyRE     = regexp.MustCompile(`^worker_messages\.rules\.[a-z][a-z0-9_]*\.(from|to|types|scope|enabled)$`)
	selectorRE    = regexp.MustCompile(`^(role|lane|agent):(.*)$`)
	fieldSuffixes = []string{"enabled", "types", "scope", "from", "to"} // longest first
	// ruleLayerOrder is the resolution order of rule layers, highest first:
	// the environment replaces a rule as a unit, then the session, project,
	// user and defaults layers of the loaded Config.
	ruleLayerOrder = []string{"env", "session", "project", "user", "defaults"}
)

// Participant identifies one side of a worker message.
type Participant struct {
	Name string // agent name (the current member of the assignment)
	Role string
	Lane string
}

// Request is one worker-to-worker message to authorize. Assigned and
// AssignmentID come from the collaboration module, which verifies the
// participants' identities and the assignment before calling Authorize.
type Request struct {
	From         Participant
	To           Participant
	Type         string
	AssignmentID string
	// Inbound is the destination's resolved inbound setting (auto|off);
	// off always wins over any rule.
	Inbound string
	// Assigned is true when the message happens inside an active assignment.
	Assigned bool
}

// Rule is one effective worker_messages rule, taken whole from the highest
// layer that names it. A partial definition in that layer is a load error;
// it is never completed from a lower layer.
type Rule struct {
	// Name is the normalized rule name, [a-z][a-z0-9_]*.
	Name string
	// Original is the name spelling in the defining layer ("" when the
	// rule is defined by the environment).
	Original string
	// Source is the defining layer: env | session | project | user | defaults.
	Source string
	// From/To are the selector lists (role:<role>, lane:<lane>,
	// agent:<name>, comma-separated, no wildcards), raw values.
	From string
	To   string
	// Types is the comma-separated list of allowed message types, raw value.
	Types string
	// Scope is the rule scope (assignment in the first version).
	Scope string
	// Enabled is false for a tombstone (enabled=off in the defining layer),
	// which deactivates a rule inherited from a lower layer.
	Enabled bool

	fromSel []selector
	toSel   []selector
	typeSet map[string]bool
}

// Policy is the resolved worker_messages policy of one Load.
type Policy struct {
	// Mode is off | policy.
	Mode string
	// Rules holds the effective rules when Mode is policy.
	Rules []Rule
}

// Errors Authorize returns for the request-level refusals.
var (
	ErrPolicyOff      = errors.New("worker messages policy is off")
	ErrInboundOff     = errors.New("destination inbound is off")
	ErrNoAssignment   = errors.New("message requires an active assignment")
	ErrNoAssignmentID = errors.New("message requires an assignment id")
)

// Load resolves the worker_messages mode and rules from the config layers
// cfg already read (defaults < user < project < session) plus the
// HERDR_SOHO_WORKER_MESSAGES_RULES_* environment variables, the highest
// rule layer that replaces a rule as a unit. The mode env var follows Cfg
// (HERDR_SOHO_WORKER_MESSAGES). An existing config file that cannot be
// read, an invalid mode, a strange rule field, a bad rule name or selector,
// an unknown type or scope, and a partial rule in the defining layer are
// visible errors. With mode off the rules are not loaded at all.
func Load(cfg *core.Config, env platform.Env) (Policy, error) {
	if err := checkReadableFiles(cfg, env); err != nil {
		return Policy{}, err
	}
	for _, layer := range cfg.Layers {
		for _, original := range layer.OriginalKeys {
			if strings.ToLower(core.NormalizeKey(original)) == modeKey && original != modeKey {
				return Policy{}, fmt.Errorf("worker_messages: strange mode key %q", original)
			}
		}
	}
	mode := core.Cfg(cfg, modeKey, ModeOff, env)
	if mode != ModeOff && mode != ModePolicy {
		return Policy{}, fmt.Errorf("worker_messages: invalid mode %q (off|policy)", mode)
	}
	if mode == ModeOff {
		return Policy{Mode: ModeOff}, nil
	}
	bySource, names, err := collectRuleLayers(cfg, env)
	if err != nil {
		return Policy{}, err
	}
	rules := make([]Rule, 0, len(names))
	for _, name := range names {
		rule, err := effectiveRule(name, bySource)
		if err != nil {
			return Policy{}, err
		}
		rules = append(rules, rule)
	}
	return Policy{Mode: ModePolicy, Rules: rules}, nil
}

// checkReadableFiles refuses an existing config file that is not readable
// (the shared LoadConfig drops such a file silently for the legacy keys;
// the policy must not).
func checkReadableFiles(cfg *core.Config, env platform.Env) error {
	if cfg == nil {
		return errors.New("worker_messages: configuration was not loaded")
	}
	for _, layer := range cfg.Layers {
		if layer.ReadError != nil {
			return fmt.Errorf("worker_messages: %s config file %s is not readable: %w", layer.Source, layer.File, layer.ReadError)
		}
	}
	return nil
}

// ruleLayer is one layer's view of the named rules: rule name -> field ->
// raw value, plus the original name spelling seen in that layer.
type ruleLayer struct {
	source   string
	fields   map[string]map[string]string
	original map[string]string
}

// collectRuleLayers gathers the per-layer rule definitions: the file layers
// come from the loaded Config (by source), the environment is the highest
// layer. names comes back sorted for deterministic resolution and output.
func collectRuleLayers(cfg *core.Config, env platform.Env) (map[string]ruleLayer, []string, error) {
	bySource := make(map[string]ruleLayer)
	for _, layer := range cfg.Layers {
		rl := ruleLayer{source: layer.Source, fields: make(map[string]map[string]string), original: make(map[string]string)}
		for _, original := range layer.OriginalKeys {
			if strings.HasPrefix(strings.ToLower(core.NormalizeKey(original)), rulePrefix) && !ruleKeyRE.MatchString(original) {
				return nil, nil, fmt.Errorf("worker_messages: strange rule key %q", original)
			}
		}
		for key, entry := range layer.Entries {
			if !strings.HasPrefix(key, rulePrefix) {
				continue
			}
			name, field, ok := parseRuleKey(key)
			if !ok || (entry.Original != "" && !ruleKeyRE.MatchString(entry.Original)) {
				shown := entry.Original
				if shown == "" {
					shown = key
				}
				return nil, nil, fmt.Errorf("worker_messages: strange rule key %q (want worker_messages.rules.<name>.%s)", shown, strings.Join([]string{"from", "to", "types", "scope", "enabled"}, "|"))
			}
			if rl.fields[name] == nil {
				rl.fields[name] = make(map[string]string)
			}
			rl.fields[name][field] = entry.Value
			if rl.original[name] == "" {
				rl.original[name] = originalRuleName(entry.Original, name)
			}
		}
		if len(rl.fields) > 0 {
			bySource[layer.Source] = rl
		}
	}
	if layer, err := envRuleLayer(env); err != nil {
		return nil, nil, err
	} else if len(layer.fields) > 0 {
		bySource[layer.source] = layer
	}
	seen := make(map[string]bool)
	for _, source := range ruleLayerOrder {
		layer := bySource[source]
		for name := range layer.fields {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return bySource, names, nil
}

// parseRuleKey splits a normalized rule key into (name, field). The field is
// the longest known suffix, so a rule named x_to keeps its field apart.
func parseRuleKey(key string) (string, string, bool) {
	rest := strings.TrimPrefix(key, rulePrefix)
	for _, field := range fieldSuffixes {
		suffix := "_" + field
		if strings.HasSuffix(rest, suffix) {
			name := strings.TrimSuffix(rest, suffix)
			if ruleNameRE.MatchString(name) {
				return name, field, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

// originalRuleName recovers the rule name spelling the defining layer wrote
// ("" when the original key is not the clean dotted four-part form).
func originalRuleName(original, name string) string {
	cleaned := strings.TrimFunc(original, func(r rune) bool { return r == ' ' || r == '\t' })
	parts := strings.Split(cleaned, ".")
	if len(parts) == 4 && parts[0] == "worker_messages" && parts[1] == "rules" {
		return parts[2]
	}
	return name
}

// envRuleLayer reads the HERDR_SOHO_WORKER_MESSAGES_RULES_* variables into
// the highest rule layer. The name between the prefix and the field suffix
// is normalized and lowercased; an empty value is unset (Cfg semantics).
func envRuleLayer(env platform.Env) (ruleLayer, error) {
	layer := ruleLayer{source: "env", fields: make(map[string]map[string]string), original: make(map[string]string)}
	for name, value := range env {
		if !strings.HasPrefix(name, EnvRulePrefix) {
			continue
		}
		rest := strings.TrimPrefix(name, EnvRulePrefix)
		field := ""
		for _, suffix := range fieldSuffixes {
			if strings.HasSuffix(rest, "_"+strings.ToUpper(suffix)) {
				field = suffix
				break
			}
		}
		if field == "" {
			return layer, fmt.Errorf("worker_messages: unknown rule field in environment %s (want %s)", name, strings.Join([]string{"FROM", "TO", "TYPES", "SCOPE", "ENABLED"}, "|"))
		}
		rawName := strings.TrimSuffix(rest, "_"+strings.ToUpper(field))
		norm := strings.ToLower(core.NormalizeKey(rawName))
		if !ruleNameRE.MatchString(norm) {
			return layer, fmt.Errorf("worker_messages: invalid rule name %q in environment %s (want [a-z][a-z0-9_]*)", norm, name)
		}
		if value == "" {
			continue
		}
		if layer.fields[norm] == nil {
			layer.fields[norm] = make(map[string]string)
		}
		layer.fields[norm][field] = value
	}
	return layer, nil
}

// effectiveRule resolves one rule as a unit from the highest layer that
// names it and validates that layer's fields.
func effectiveRule(name string, bySource map[string]ruleLayer) (Rule, error) {
	for _, source := range ruleLayerOrder {
		layer, defined := bySource[source]
		if !defined {
			continue
		}
		fields, defined := layer.fields[name]
		if !defined {
			continue
		}
		rule := Rule{Name: name, Source: layer.source, Original: layer.original[name]}
		if value, ok := fields["enabled"]; ok {
			if value != "on" && value != "off" {
				return Rule{}, fmt.Errorf("worker_messages: rule %q in %s layer has invalid enabled %q (on|off)", name, source, value)
			}
			rule.Enabled = value == "on"
		} else {
			rule.Enabled = true
		}
		// Deterministic field order for validation and the error message.
		for _, field := range []string{"from", "to", "types", "scope", "enabled"} {
			value, ok := fields[field]
			if !ok {
				continue
			}
			switch field {
			case "from":
				sel, err := parseSelectors(value)
				if err != nil {
					return Rule{}, fmt.Errorf("worker_messages: rule %q from in %s layer: %v", name, source, err)
				}
				rule.From = value
				rule.fromSel = sel
			case "to":
				sel, err := parseSelectors(value)
				if err != nil {
					return Rule{}, fmt.Errorf("worker_messages: rule %q to in %s layer: %v", name, source, err)
				}
				rule.To = value
				rule.toSel = sel
			case "types":
				types, err := parseTypes(value)
				if err != nil {
					return Rule{}, fmt.Errorf("worker_messages: rule %q types in %s layer: %v", name, source, err)
				}
				rule.Types = value
				rule.typeSet = types
			case "scope":
				if value != scopeAssignment {
					return Rule{}, fmt.Errorf("worker_messages: rule %q scope in %s layer: unknown scope %q (assignment)", name, source, value)
				}
				rule.Scope = value
			case "enabled":
				// Validated above.
			}
		}
		if rule.Enabled {
			missing := make([]string, 0, 4)
			for _, field := range []string{"from", "to", "types", "scope"} {
				if _, ok := fields[field]; !ok {
					missing = append(missing, field)
				}
			}
			if len(missing) > 0 {
				return Rule{}, fmt.Errorf("worker_messages: rule %q in %s layer is partial (missing %s); a rule is taken whole from its layer, never completed from a lower one", name, source, strings.Join(missing, ", "))
			}
		}
		return rule, nil
	}
	return Rule{}, nil
}

type selector struct {
	kind  string // role | lane | agent
	value string
}

// parseSelectors validates one comma-separated selector list: each item is
// role:<role>, lane:<lane> or agent:<name>; empty, duplicated or malformed
// items (and wildcards, unsupported in the first version) fail.
func parseSelectors(value string) ([]selector, error) {
	parts := strings.Split(value, ",")
	seen := make(map[string]bool, len(parts))
	out := make([]selector, 0, len(parts))
	for _, raw := range parts {
		item := strings.TrimSpace(raw)
		if item == "" {
			return nil, fmt.Errorf("empty selector in %q", value)
		}
		if seen[item] {
			return nil, fmt.Errorf("duplicated selector %q", item)
		}
		seen[item] = true
		match := selectorRE.FindStringSubmatch(item)
		if match == nil {
			return nil, fmt.Errorf("malformed selector %q (want role:<role>, lane:<lane> or agent:<name>)", item)
		}
		kind, val := match[1], match[2]
		if val == "" {
			return nil, fmt.Errorf("empty selector value %q (want role:<role>, lane:<lane> or agent:<name>)", item)
		}
		if strings.TrimSpace(val) != val || strings.ContainsAny(val, "* \t") {
			return nil, fmt.Errorf("invalid selector value in %q (wildcards and blank spaces are not supported)", item)
		}
		out = append(out, selector{kind: kind, value: val})
	}
	return out, nil
}

// parseTypes validates one comma-separated message type list against the
// first version's types.
func parseTypes(value string) (map[string]bool, error) {
	parts := strings.Split(value, ",")
	out := make(map[string]bool, len(parts))
	for _, raw := range parts {
		typeName := strings.TrimSpace(raw)
		if typeName == "" {
			return nil, fmt.Errorf("empty type in %q", value)
		}
		if !contains(MessageTypes, typeName) {
			return nil, fmt.Errorf("unknown message type %q (allowed: %s)", typeName, strings.Join(MessageTypes, ", "))
		}
		out[typeName] = true
	}
	return out, nil
}

// Authorize applies the policy to one request. A message is allowed only
// when an enabled rule matches the sender, the destination and the type:
// the rule is directional (a from/to pair does not authorize the reverse),
// the message happens inside an active assignment (scope assignment), and
// the destination's inbound setting is not off.
func (p Policy) Authorize(req Request) error {
	if p.Mode != ModePolicy {
		return ErrPolicyOff
	}
	if req.Inbound == "off" {
		return ErrInboundOff
	}
	if !req.Assigned {
		return ErrNoAssignment
	}
	if req.AssignmentID == "" {
		return ErrNoAssignmentID
	}
	for i := range p.Rules {
		rule := &p.Rules[i]
		if !rule.Enabled {
			continue
		}
		if !matches(rule.fromSel, req.From) || !matches(rule.toSel, req.To) {
			continue
		}
		if !rule.typeSet[req.Type] {
			continue
		}
		return nil
	}
	return fmt.Errorf("no worker_messages rule allows %s to %s with type %s", describe(req.From), describe(req.To), req.Type)
}

func matches(selectors []selector, participant Participant) bool {
	for _, sel := range selectors {
		switch sel.kind {
		case "role":
			if participant.Role == sel.value {
				return true
			}
		case "lane":
			if participant.Lane == sel.value {
				return true
			}
		case "agent":
			if participant.Name == sel.value {
				return true
			}
		}
	}
	return false
}

func describe(participant Participant) string {
	if participant.Name != "" {
		return "agent " + participant.Name
	}
	if participant.Role != "" {
		return "role " + participant.Role
	}
	if participant.Lane != "" {
		return "lane " + participant.Lane
	}
	return "unknown participant"
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
