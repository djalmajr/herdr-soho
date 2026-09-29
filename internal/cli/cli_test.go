package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Every command the JS knows has a Go case since D5b, so runJS is reached
// only by a known command without one. The fallback tests register such a
// command in the test binary (the helper process is this binary too).
func init() { knownCommands["not-ported"] = true }

func TestRunFallback(t *testing.T) {
	// JS (fora): "run-tests: missing jq exits 2 with Node-only guidance" — o teste executa o preflight do runner de suítes Bash, um detalhe do harness JavaScript.
	// JS (fora): "Bash suite: missing jq exits 2 with a direct dependency message" — o teste invoca diretamente uma suíte Bash para validar seu pré-requisito.
	// Mutation captured: running the fallback as a child changes signal delivery and leaks copied env values.
	if runtime.GOOS == "windows" {
		t.Skip("fallback shims are POSIX test fixtures")
	}
	t.Run("Unix fallback replaces the process and preserves args, stdin, env, and child code", func(t *testing.T) {
		bin := t.TempDir()
		record := filepath.Join(t.TempDir(), "args")
		inputRecord := filepath.Join(t.TempDir(), "stdin")
		node := filepath.Join(bin, "node")
		script := "#!/bin/sh\nif [ \"$1\" = \"-e\" ]; then printf 20; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$RECORD\"\nprintf '%s' \"${HERDR_SOHO_SOCKET_PATH-unset}\" >> \"$RECORD\"\n/bin/cat > \"$INPUT_RECORD\"\nprintf 'child-output\\n'\nexit 17\n"
		if err := os.WriteFile(node, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		env := map[string]string{"PATH": bin, "RECORD": record, "INPUT_RECORD": inputRecord, "HERDR_SOHO_SKILL_DIR": "/skill", "HERDR_AGENTS_SOCKET_PATH": "legacy"}
		stdout, stderr, code := runFallbackHelper(t, os.Args[0], []string{"not-ported", "a b", "--x"}, env, "stdin payload")
		if code != 17 || stdout != "child-output\n" || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		data, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		want := "/skill/scripts/herdr-soho.mjs\nnot-ported\na b\n--x\nunset"
		if string(data) != want {
			t.Fatalf("args:\n%s\nwant:\n%s", data, want)
		}
		stdin, err := os.ReadFile(inputRecord)
		if err != nil || string(stdin) != "stdin payload" {
			t.Fatalf("stdin = %q, %v", stdin, err)
		}
	})
	t.Run("Node below 20 falls back to Bun", func(t *testing.T) {
		bin := t.TempDir()
		record := filepath.Join(t.TempDir(), "bun-args")
		if err := os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\nprintf 18\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		bun := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RECORD\"\nexit 23\n"
		if err := os.WriteFile(filepath.Join(bin, "bun"), []byte(bun), 0o700); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runFallbackHelper(t, os.Args[0], []string{"not-ported"}, map[string]string{"PATH": bin, "RECORD": record, "HERDR_SOHO_SKILL_DIR": "/skill"}, "")
		if code != 23 || stdout != "" || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		data, err := os.ReadFile(record)
		if err != nil || string(data) != "/skill/scripts/herdr-soho.mjs\nnot-ported\n" {
			t.Fatalf("bun args = %q, %v", data, err)
		}
	})
	t.Run("absolute HERDR_SOHO_JS_RUNTIME runs without a version probe", func(t *testing.T) {
		bin := t.TempDir()
		runtime := filepath.Join(bin, "custom-node")
		record := filepath.Join(t.TempDir(), "runtime-args")
		if err := os.WriteFile(runtime, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RECORD\"\nexit 19\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, stderr, code := runFallbackHelper(t, os.Args[0], []string{"not-ported"}, map[string]string{"PATH": t.TempDir(), "RECORD": record, "HERDR_SOHO_JS_RUNTIME": runtime, "HERDR_SOHO_SKILL_DIR": "/skill"}, "")
		if code != 19 || stderr != "" {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		data, err := os.ReadFile(record)
		if err != nil || string(data) != "/skill/scripts/herdr-soho.mjs\nnot-ported\n" {
			t.Fatalf("override args = %q, %v", data, err)
		}
	})
	t.Run("Unix fallback process receives the child's termination signal", func(t *testing.T) {
		bin := t.TempDir()
		node := "#!/bin/sh\nif [ \"$1\" = \"-e\" ]; then printf 20; exit 0; fi\nkill -TERM $$\n"
		if err := os.WriteFile(filepath.Join(bin, "node"), []byte(node), 0o700); err != nil {
			t.Fatal(err)
		}
		_, stderr, code := runFallbackHelper(t, os.Args[0], []string{"not-ported"}, map[string]string{"PATH": bin, "HERDR_SOHO_SKILL_DIR": "/skill"}, "")
		if code != -1 || stderr != "" {
			t.Fatalf("signaled child wrapper code=%d stderr=%q", code, stderr)
		}
	})
	t.Run("skill root is discovered above the executable", func(t *testing.T) {
		root := t.TempDir()
		scripts := filepath.Join(root, "scripts")
		bin := filepath.Join(t.TempDir(), "bin")
		if err := os.Mkdir(bin, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "roles"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(scripts, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("skill"), 0o600); err != nil {
			t.Fatal(err)
		}
		executable := filepath.Join(scripts, "helper")
		if err := copyFile(os.Args[0], executable); err != nil {
			t.Fatal(err)
		}
		record := filepath.Join(t.TempDir(), "args")
		node := "#!/bin/sh\nif [ \"$1\" = \"-e\" ]; then printf 20; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$RECORD\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(bin, "node"), []byte(node), 0o700); err != nil {
			t.Fatal(err)
		}
		physicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		_, stderr, code := runFallbackHelper(t, executable, []string{"not-ported"}, map[string]string{"PATH": bin, "RECORD": record}, "")
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		data, err := os.ReadFile(record)
		if err != nil || string(data) != filepath.Join(physicalRoot, "scripts", "herdr-soho.mjs")+"\nnot-ported\n" {
			t.Fatalf("discovered entry = %q, %v", data, err)
		}
	})
	t.Run("reports missing Node and Bun with launcher message and code 2", func(t *testing.T) {
		bin := t.TempDir()
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var out, errOut bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &errOut
		defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
		got := Run([]string{"not-ported"}, platform.Env{"PATH": bin, "HERDR_SOHO_SKILL_DIR": "/skill"})
		if got != 2 || out.Len() != 0 || errOut.String() != "herdr-soho: needs Node.js 20+ or Bun\n" {
			t.Fatalf("got %d out=%q err=%q", got, out.String(), errOut.String())
		}
	})
}

func TestFallbackExecHelper(t *testing.T) {
	if os.Getenv("HERDR_GO_FALLBACK_HELPER") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("HERDR_GO_FALLBACK_ARGS")), &args); err != nil {
		os.Exit(99)
	}
	os.Exit(Run(args, platform.EnvFromOS()))
}

func runFallbackHelper(t *testing.T, executable string, args []string, env map[string]string, stdin string) (string, string, int) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestFallbackExecHelper$")
	cmd.Env = []string{"HERDR_GO_FALLBACK_HELPER=1", "HERDR_GO_FALLBACK_ARGS=" + string(encoded)}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("fallback helper failed: %v stderr=%s", err, stderr.String())
	return "", "", -1
}

func copyFile(source, dest string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o700)
}

func TestHelpAndUnknown(t *testing.T) {
	t.Run("help variants print the usage", func(t *testing.T) { // JS: "parity: help, -h, --help and no command print the usage (rc 0)"
		for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {}} {
			var out, errOut bytes.Buffer
			oldOut, oldErr := platform.Stdout, platform.Stderr
			platform.Stdout, platform.Stderr = &out, &errOut
			code := Run(args, platform.Env{})
			platform.Stdout, platform.Stderr = oldOut, oldErr
			if code != 0 || out.String() != usage || errOut.Len() != 0 {
				t.Errorf("args=%v code=%d err=%q", args, code, errOut.String())
			}
		}
	})
	t.Run("unknown command uses the Bash-compatible message", func(t *testing.T) { // JS: "parity: an unknown command dies 2 with the bash message"
		// Mutation captured: dropping or changing the unknown-command diagnostic changes both its exit and exact stderr.
		var errOut bytes.Buffer
		oldErr := platform.Stderr
		platform.Stderr = &errOut
		code := Run([]string{"nope"}, platform.Env{})
		platform.Stderr = oldErr
		if code != 2 || errOut.String() != "herdr-soho: unknown command 'nope'\n" {
			t.Fatalf("code=%d err=%q", code, errOut.String())
		}
	})
	if !strings.Contains(usage, "herdr-soho — role-agent layer") {
		t.Fatal("missing usage text")
	}
}

func TestNowriteAndLegacyEnv(t *testing.T) {
	// Mutation captured: allowing extra arguments after doctor or roster bypasses NOWRITE.
	var errOut bytes.Buffer
	oldErr := platform.Stderr
	platform.Stderr = &errOut
	code := Run([]string{"kinds"}, platform.Env{"HERDR_SOHO_NOWRITE": "1", "HERDR_AGENTS_SOCKET_PATH": "legacy"})
	platform.Stderr = oldErr
	if code != 2 || !strings.Contains(errOut.String(), "HERDR_SOHO_NOWRITE=1 is read-only") || !strings.Contains(errOut.String(), "rejected: kinds") {
		t.Fatalf("code=%d err=%q", code, errOut.String())
	}
	if got := applyLegacyForTest(platform.Env{"HERDR_AGENTS_SOCKET_PATH": "legacy"}); got != "legacy" {
		t.Fatalf("legacy mapped to %q", got)
	}
	for _, args := range [][]string{{"doctor", "--fix"}, {"roster", "extra"}} {
		var stderr bytes.Buffer
		oldErr := platform.Stderr
		platform.Stderr = &stderr
		got := Run(args, platform.Env{"HERDR_SOHO_NOWRITE": "1"})
		platform.Stderr = oldErr
		if got != 2 || !strings.Contains(stderr.String(), "extra argument") {
			t.Errorf("NOWRITE args=%v code=%d stderr=%q", args, got, stderr.String())
		}
	}
	var stderr bytes.Buffer
	oldErr = platform.Stderr
	platform.Stderr = &stderr
	got := Run([]string{"kinds"}, platform.Env{"HERDR_AGENTS_NOWRITE": "1"})
	platform.Stderr = oldErr
	if got != 2 || !strings.Contains(stderr.String(), "HERDR_SOHO_NOWRITE=1") {
		t.Errorf("legacy NOWRITE code=%d stderr=%q", got, stderr.String())
	}
}

func applyLegacyForTest(env platform.Env) string {
	applyLegacyEnv(env)
	return env.Get("HERDR_SOHO_SOCKET_PATH")
}
