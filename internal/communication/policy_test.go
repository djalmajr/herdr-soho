package communication

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// policyFixture isolates the config stack: user under XDG_CONFIG_HOME,
// project under repo/.agents, session under HERDR_SOHO_DIR, defaults from
// the skill checked out with this tree. The process chdirs to repo so
// Load resolves the same files LoadConfig read.
func policyFixture(t *testing.T) (platform.Env, string) {
	t.Helper()
	skill := skillDir(t) // before the chdir: the test binary's cwd is internal/communication
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	state := filepath.Join(root, "state")
	for _, dir := range []string{repo, home, conf, state} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(repo)
	env := platform.Env{
		"HOME": home, "XDG_CONFIG_HOME": conf, "HERDR_SOHO_DIR": state,
		"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws",
		"PATH": os.Getenv("PATH"),
	}
	return env, repo
}

var policyPackageDir, policyPackageDirErr = os.Getwd()

func skillDir(t *testing.T) string {
	t.Helper()
	// Runtime location stays valid when the test binary is cross-compiled
	// and moved; fixtures may chdir repeatedly after this initial capture.
	if policyPackageDirErr != nil {
		t.Fatal(policyPackageDirErr)
	}
	skill := filepath.Join(policyPackageDir, "..", "..", "skills", "herdr-soho")
	if _, err := os.Stat(filepath.Join(skill, "SKILL.md")); err != nil {
		t.Fatalf("skill directory %q not found: %v", skill, err)
	}
	return skill
}

func writePolicyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustLoad(t *testing.T, env platform.Env, cwd string) Policy {
	t.Helper()
	ctx := core.LoadConfig(env, cwd)
	pol, err := Load(&ctx, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return pol
}

func loadExpectErr(t *testing.T, env platform.Env, cwd, want string) {
	t.Helper()
	ctx := core.LoadConfig(env, cwd)
	if _, err := Load(&ctx, env); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Load error = %v, want a visible error containing %q", err, want)
	}
}

func osGetwdOrDie(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func ruleOf(t *testing.T, pol Policy, name string) Rule {
	t.Helper()
	for _, rule := range pol.Rules {
		if rule.Name == name {
			return rule
		}
	}
	t.Fatalf("rule %q not in policy (rules: %d)", name, len(pol.Rules))
	return Rule{}
}

func reqFromTo(from, to Participant, messageType string) Request {
	return Request{From: from, To: to, Type: messageType, AssignmentID: "a1", Inbound: "auto", Assigned: true}
}

func TestAuthorizeMatrix(t *testing.T) {
	// The rule authorizes exactly the declared trip, type and participants.
	env, cwd := policyFixture(t)
	writePolicyFile(t, platform.UserConfigPath(platform.Current(), env),
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer,lane:build\n"+
			"worker_messages.rules.review.to=role:reviewer,agent:rev\n"+
			"worker_messages.rules.review.types=review.ready,review.question\n"+
			"worker_messages.rules.review.scope=assignment\n")
	pol := mustLoad(t, env, cwd)

	allowed := []Request{
		reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.ready"),
		reqFromTo(Participant{Role: "implementer", Lane: "build"}, Participant{Name: "rev"}, "review.question"),
		reqFromTo(Participant{Lane: "build"}, Participant{Role: "reviewer"}, "review.ready"),
	}
	for i, req := range allowed {
		if err := pol.Authorize(req); err != nil {
			t.Fatalf("request %d must be allowed: %v", i, err)
		}
	}

	denied := []Request{
		// The reverse trip is not authorized implicitly.
		reqFromTo(Participant{Role: "reviewer"}, Participant{Role: "implementer"}, "review.ready"),
		// Another role / lane / agent than the rule names.
		reqFromTo(Participant{Role: "designer"}, Participant{Role: "reviewer"}, "review.ready"),
		reqFromTo(Participant{Lane: "review"}, Participant{Role: "reviewer"}, "review.ready"),
		reqFromTo(Participant{Role: "implementer"}, Participant{Name: "other"}, "review.ready"),
		reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.finding"),
		reqFromTo(Participant{}, Participant{Role: "reviewer"}, "review.ready"),
		reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "custom.type"),
	}
	for i, req := range denied {
		err := pol.Authorize(req)
		if err == nil {
			t.Fatalf("denied request %d was allowed (no rule should match)", i)
		}
		if errors.Is(err, ErrInboundOff) || errors.Is(err, ErrNoAssignment) || errors.Is(err, ErrNoAssignmentID) {
			t.Fatalf("denied request %d: wrong refusal %v (want no matching rule)", i, err)
		}
	}
}

func TestCompleteDisabledRuleCannotAuthorize(t *testing.T) {
	env, cwd := policyFixture(t)
	writePolicyFile(t, platform.UserConfigPath(platform.Current(), env), "worker_messages=policy\nworker_messages.rules.r.from=role:implementer\nworker_messages.rules.r.to=role:reviewer\nworker_messages.rules.r.types=review.ready\nworker_messages.rules.r.scope=assignment\nworker_messages.rules.r.enabled=off\n")
	pol := mustLoad(t, env, cwd)
	if err := pol.Authorize(reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.ready")); err == nil {
		t.Fatal("disabled complete rule authorized")
	}
}

func TestMalformedOriginalRuleKeysCannotAlias(t *testing.T) {
	for _, key := range []string{"worker_messages.rules.review.request.enabled", "worker_messages.rules.review-request.enabled", "worker_messages_rules_review_request_enabled", "Worker_messages.rules.r.enabled", "WORKER_MESSAGES.RULES.R.ENABLED"} {
		t.Run(key, func(t *testing.T) {
			env, cwd := policyFixture(t)
			writePolicyFile(t, platform.UserConfigPath(platform.Current(), env), "worker_messages=policy\n"+key+"=off\nworker_messages.rules.review_request.enabled=off\n")
			loadExpectErr(t, env, cwd, "strange rule key")
		})
	}
}

func TestMalformedModeKeyCannotSilentlyDropDisable(t *testing.T) {
	env, cwd := policyFixture(t)
	writePolicyFile(t, platform.UserConfigPath(platform.Current(), env), "worker_messages=policy\nWorker_messages=off\n")
	loadExpectErr(t, env, cwd, "strange mode key")
}

func TestAssignmentAndInbound(t *testing.T) {
	env, cwd := policyFixture(t)
	writePolicyFile(t, platform.UserConfigPath(platform.Current(), env),
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	pol := mustLoad(t, env, cwd)
	req := reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.ready")

	req.Assigned = false
	if err := pol.Authorize(req); !errors.Is(err, ErrNoAssignment) {
		t.Fatalf("Assigned=false: got %v, want ErrNoAssignment", err)
	}
	req.Assigned = true
	req.AssignmentID = ""
	if err := pol.Authorize(req); !errors.Is(err, ErrNoAssignmentID) {
		t.Fatalf("empty AssignmentID: got %v, want ErrNoAssignmentID", err)
	}
	req.AssignmentID = "a1"
	req.Inbound = "off"
	if err := pol.Authorize(req); !errors.Is(err, ErrInboundOff) {
		t.Fatalf("inbound off must win over the matching rule: got %v, want ErrInboundOff", err)
	}
	for _, inbound := range []string{"auto", ""} {
		req.Inbound = inbound
		if err := pol.Authorize(req); err != nil {
			t.Fatalf("inbound %q: got %v, want allowed", inbound, err)
		}
	}
}

func TestPolicyOff(t *testing.T) {
	env, cwd := policyFixture(t)
	// No worker_messages set: the skill defaults keep off, and the broken
	// rule keys below are not even loaded.
	writePolicyFile(t, platform.UserConfigPath(platform.Current(), env),
		"worker_messages.rules.broken.from=role:implementer\n"+
			"worker_messages.rules.broken.to=role:reviewer\n"+
			"worker_messages.rules.broken.types=review.ready\n"+
			"worker_messages.rules.broken.scope=assignment\n")
	pol := mustLoad(t, env, cwd)
	if pol.Mode != ModeOff || len(pol.Rules) != 0 {
		t.Fatalf("policy = %+v, want mode off with no rules loaded", pol)
	}
	err := pol.Authorize(reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.ready"))
	if !errors.Is(err, ErrPolicyOff) {
		t.Fatalf("mode off: got %v, want ErrPolicyOff", err)
	}
}

func TestLoadRuleFailures(t *testing.T) {
	base := "worker_messages=policy\n"
	cases := []struct {
		name string
		file string
		want string
	}{
		{"wildcard selector", base + "worker_messages.rules.x.from=role:*\nworker_messages.rules.x.to=role:y\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\n", "wildcard"},
		{"bare wildcard", base + "worker_messages.rules.x.from=*\nworker_messages.rules.x.to=role:y\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\n", "malformed selector"},
		{"unknown type", base + "worker_messages.rules.x.from=role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready,review.bogus\nworker_messages.rules.x.scope=assignment\n", "unknown message type"},
		{"empty type", base + "worker_messages.rules.x.from=role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready,\nworker_messages.rules.x.scope=assignment\n", "empty type"},
		{"unknown scope", base + "worker_messages.rules.x.from=role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=global\n", "unknown scope"},
		{"invalid enabled", base + "worker_messages.rules.x.from=role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\nworker_messages.rules.x.enabled=maybe\n", "invalid enabled"},
		{"strange rule field", base + "worker_messages.rules.x.from=role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\nworker_messages.rules.x.bogus=1\n", "strange rule key"},
		{"empty selector value", base + "worker_messages.rules.x.from=role:\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\n", "empty selector"},
		{"duplicated selector", base + "worker_messages.rules.x.from=role:a,role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\n", "duplicated selector"},
		{"selector with space", base + "worker_messages.rules.x.from=role:a b\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\nworker_messages.rules.x.scope=assignment\n", "invalid selector value"},
		{"bad rule name", base + "worker_messages.rules.9x.from=role:a\nworker_messages.rules.9x.to=role:b\nworker_messages.rules.9x.types=review.ready\nworker_messages.rules.9x.scope=assignment\n", "strange rule key"},
		{"partial rule", base + "worker_messages.rules.x.from=role:a\nworker_messages.rules.x.to=role:b\nworker_messages.rules.x.types=review.ready\n", "partial (missing scope)"},
		{"tombstone stays valid with one field", "", ""}, // covered by TestTombstone
	}
	for _, tc := range cases {
		if tc.file == "" {
			continue
		}
		env, cwd := policyFixture(t)
		writePolicyFile(t, platform.UserConfigPath(platform.Current(), env), tc.file)
		t.Run(tc.name, func(t *testing.T) {
			loadExpectErr(t, env, cwd, tc.want)
		})
	}
}

func TestPartialHigherLayerRefused(t *testing.T) {
	// The higher layer names the rule, so its partial definition fails: it
	// is not completed with the complete lower definition.
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	writePolicyFile(t, filepath.Join(cwd, ".agents", "herdr-soho.conf"),
		"worker_messages.rules.review.types=review.result\n")
	loadExpectErr(t, env, cwd, "partial (missing from, to, scope)")
	// And the partial layer must be the one named in the error (project).
	ctx := core.LoadConfig(env, cwd)
	if _, err := Load(&ctx, env); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("error = %v, want it to name the project layer", err)
	}
}

func TestTombstone(t *testing.T) {
	// enabled=off alone in the session layer deactivates the inherited rule.
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	state := env.Get("HERDR_SOHO_DIR")
	writePolicyFile(t, filepath.Join(state, "ws", "session.conf"),
		"worker_messages.rules.review.enabled=off\n")
	pol := mustLoad(t, env, cwd)
	rule := ruleOf(t, pol, "review")
	if rule.Source != "session" || rule.Enabled {
		t.Fatalf("rule = %+v, want the session tombstone (Enabled=false)", rule)
	}
	req := reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.ready")
	if err := pol.Authorize(req); err == nil {
		t.Fatal("tombstoned rule must refuse the message")
	}
	// A tombstone with the full field set also deactivates (no partial error
	// for the fields that are present).
	writePolicyFile(t, filepath.Join(state, "ws", "session.conf"),
		"worker_messages.rules.review.enabled=off\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	pol = mustLoad(t, env, cwd)
	if rule := ruleOf(t, pol, "review"); rule.Source != "session" || rule.Enabled {
		t.Fatalf("rule = %+v, want the session tombstone", rule)
	}
	// But a tombstone with a malformed present field still fails.
	writePolicyFile(t, filepath.Join(state, "ws", "session.conf"),
		"worker_messages.rules.review.enabled=off\n"+
			"worker_messages.rules.review.from=role:*\n")
	loadExpectErr(t, env, cwd, "wildcard")
}

func TestExplicitEnabledOn(t *testing.T) {
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n"+
			"worker_messages.rules.review.enabled=on\n")
	pol := mustLoad(t, env, cwd)
	if rule := ruleOf(t, pol, "review"); !rule.Enabled {
		t.Fatalf("rule = %+v, want enabled", rule)
	}
	req := reqFromTo(Participant{Role: "implementer"}, Participant{Role: "reviewer"}, "review.ready")
	if err := pol.Authorize(req); err != nil {
		t.Fatalf("explicit enabled=on: got %v, want allowed", err)
	}
}

func TestEnvRuleLayer(t *testing.T) {
	// The environment is the highest rule layer and replaces the file rule
	// as a unit; names are normalized and lowercased.
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	env["HERDR_SOHO_WORKER_MESSAGES_RULES_REVIEW_REQUEST_FROM"] = "role:env"
	env["HERDR_SOHO_WORKER_MESSAGES_RULES_REVIEW_REQUEST_TO"] = "role:env"
	env["HERDR_SOHO_WORKER_MESSAGES_RULES_REVIEW_REQUEST_TYPES"] = "review.result"
	env["HERDR_SOHO_WORKER_MESSAGES_RULES_REVIEW_REQUEST_SCOPE"] = "assignment"
	pol := mustLoad(t, env, cwd)
	rule := ruleOf(t, pol, "review_request")
	if rule.Source != "env" || rule.From != "role:env" || rule.To != "role:env" || rule.Types != "review.result" {
		t.Fatalf("rule = %+v, want the whole env rule with the lowercase name", rule)
	}
	// The file rule keeps its own identity and fields (no mixing).
	if rule := ruleOf(t, pol, "review"); rule.Source != "user" || rule.From != "role:implementer" {
		t.Fatalf("file rule = %+v, want it untouched by the env layer", rule)
	}
	req := reqFromTo(Participant{Role: "env"}, Participant{Role: "env"}, "review.result")
	if err := pol.Authorize(req); err != nil {
		t.Fatalf("env rule: got %v, want allowed", err)
	}
	// A partial env rule replaces the file rule and fails visibly.
	env["HERDR_SOHO_WORKER_MESSAGES_RULES_REVIEW_FROM"] = "role:env"
	loadExpectErr(t, env, cwd, "partial (missing to, types, scope)")
}

func TestEnvRuleFailures(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"unknown suffix", map[string]string{"HERDR_SOHO_WORKER_MESSAGES_RULES_X_BOGUS": "1"}, "unknown rule field"},
		{"bad name", map[string]string{"HERDR_SOHO_WORKER_MESSAGES_RULES_9X_FROM": "role:a"}, "invalid rule name"},
		{"no name", map[string]string{"HERDR_SOHO_WORKER_MESSAGES_RULES_FROM": "role:a"}, "unknown rule field"},
		{"invalid selector value", map[string]string{
			"HERDR_SOHO_WORKER_MESSAGES_RULES_X_FROM":  "role:*",
			"HERDR_SOHO_WORKER_MESSAGES_RULES_X_TO":    "role:b",
			"HERDR_SOHO_WORKER_MESSAGES_RULES_X_TYPES": "review.ready",
			"HERDR_SOHO_WORKER_MESSAGES_RULES_X_SCOPE": "assignment",
		}, "wildcard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := policyFixture(t)
			writePolicyFile(t, platform.UserConfigPath(platform.Current(), env), "worker_messages=policy\n")
			for k, v := range tc.vars {
				env[k] = v
			}
			ctx := core.LoadConfig(env, osGetwdOrDie(t))
			if _, err := Load(&ctx, env); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want a visible error containing %q", err, tc.want)
			}
		})
	}
}

func TestEnvRuleEmptyValueUnset(t *testing.T) {
	// An empty environment value is unset (Cfg semantics): the file rule
	// stays effective and complete.
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	env["HERDR_SOHO_WORKER_MESSAGES_RULES_REVIEW_FROM"] = ""
	pol := mustLoad(t, env, cwd)
	if rule := ruleOf(t, pol, "review"); rule.Source != "user" {
		t.Fatalf("rule source = %q, want user (empty env value is unset)", rule.Source)
	}
}

func TestRuleNameHoldingFieldSuffix(t *testing.T) {
	// A rule named x_to keeps its fields apart (the field is the longest
	// known suffix of the key).
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.x_to.from=role:a\n"+
			"worker_messages.rules.x_to.to=role:b\n"+
			"worker_messages.rules.x_to.types=review.ready\n"+
			"worker_messages.rules.x_to.scope=assignment\n")
	pol := mustLoad(t, env, cwd)
	if rule := ruleOf(t, pol, "x_to"); rule.Source != "user" {
		t.Fatalf("rule x_to = %+v, want it resolved from the user layer", rule)
	}
	req := reqFromTo(Participant{Role: "a"}, Participant{Role: "b"}, "review.ready")
	if err := pol.Authorize(req); err != nil {
		t.Fatalf("rule x_to: got %v, want allowed", err)
	}
}

func TestUnreadableExistingFileRefused(t *testing.T) {
	// The policy refuses an existing config file it cannot read, while the
	// legacy Cfg resolution keeps the lower layer (compatibility).
	env, cwd := policyFixture(t)
	user := platform.UserConfigPath(platform.Current(), env)
	writePolicyFile(t, user,
		"worker_messages=policy\n"+
			"worker_messages.rules.review.from=role:implementer\n"+
			"worker_messages.rules.review.to=role:reviewer\n"+
			"worker_messages.rules.review.types=review.ready\n"+
			"worker_messages.rules.review.scope=assignment\n")
	project := filepath.Join(cwd, ".agents", "herdr-soho.conf")
	// A directory at the expected file path is unreadable as configuration
	// on every supported OS, including Windows and privileged test users.
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := core.LoadConfig(env, cwd)
	if got := core.Cfg(&ctx, "worker_messages", "", env); got != "policy" {
		t.Fatalf("legacy mode = %q, want policy (the silent drop kept the user layer)", got)
	}
	if _, err := Load(&ctx, env); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("Load error = %v, want the unreadable project file named", err)
	}
}
