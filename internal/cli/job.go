package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/job"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// jobStdin is the stdin source for the job subcommands' "-" bodies; tests
// swap it.
var jobStdin io.Reader = os.Stdin

// jobFlagSpec names the flags each job subcommand accepts. The writing
// subcommands validate their inputs here and refuse with exit 2 until the
// phase 2 side effects land.
var jobFlagSpec = map[string]struct {
	valued []string
	bare   []string
}{
	"status":     {valued: []string{"--id"}},
	"wait":       {valued: []string{"--id", "--timeout"}},
	"events":     {valued: []string{"--id", "--since", "--wait"}},
	"collect":    {valued: []string{"--id"}, bare: []string{"--md", "--verify"}},
	"list":       {valued: []string{"--state"}},
	"start":      {valued: []string{"--id", "--repo", "--base", "--mode", "--brief", "--timeout"}, bare: []string{"--dry-run"}},
	"ack":        {valued: []string{"--id", "--upto"}},
	"close":      {valued: []string{"--id"}, bare: []string{"--force"}},
	"note":       {valued: []string{"--id", "--tipo", "--escopo", "--motivo", "--refs"}},
	"amend":      {valued: []string{"--id"}},
	"send":       {valued: []string{"--id"}},
	"cancel":     {valued: []string{"--id", "--grace"}},
	"checkpoint": {valued: []string{"--id"}},
	"supervise":  {valued: []string{"--id"}},
}

var jobNoteTypes = map[string]bool{
	"checkpoint": true, "question": true, "decision": true, "note": true,
}

var (
	jobRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)
	jobBasePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
)

const (
	jobAmendBodyLimit = 64 << 10
	jobSendBodyLimit  = 16 << 10
	jobNoteBodyLimit  = 16 << 10
)

// jobLifecycleStates are the contract lifecycle states accepted by --state.
var jobLifecycleStates = map[string]bool{
	job.StatusAccepted: true, job.StatusPreparing: true, job.StatusRunning: true,
	job.StatusBlocked: true, job.StatusFinishing: true, job.StatusDone: true,
	job.StatusFailed: true, job.StatusTimeout: true, job.StatusCanceled: true,
	job.StatusCollected: true, job.StatusClosed: true,
}

const jobWaitBoundMS = 600000

// cmdJob dispatches the job command family. It runs outside Herdr and never
// calls Herdr: the read subcommands (status, wait, events, collect, list)
// read files only; the writing subcommands validate their inputs and, in
// this build, only start --dry-run, ack, close and note change state.
func cmdJob(args []string, env platform.Env, cwd string) int {
	if len(args) == 0 {
		platform.Die("usage: job <status|wait|events|collect|list> --id <id> [--flags]", 2)
	}
	sub := args[0]
	rest := args[1:]
	if _, ok := jobFlagSpec[sub]; !ok {
		platform.Die("job: unknown subcommand '"+sub+"'", 2)
	}
	switch sub {
	case "status":
		return jobStatus(rest, env)
	case "wait":
		return jobWait(rest, env)
	case "events":
		return jobEvents(rest, env)
	case "collect":
		return jobCollect(rest, env)
	case "list":
		return jobList(rest, env)
	case "start":
		return jobStart(rest, env, cwd)
	case "ack":
		return jobAck(rest, env)
	case "close":
		return jobClose(rest, env)
	case "note":
		return jobNote(rest, env)
	case "amend":
		return jobAmend(rest, env)
	case "send":
		return jobSend(rest, env)
	case "cancel":
		return jobCancel(rest, env)
	case "checkpoint":
		return jobCheckpoint(rest, env)
	default:
		return jobRefusePhase2(rest, env)
	}
}

// parseJobFlags parses one subcommand's flags strictly: only the spec's
// flags, in "--flag value" or "--flag=value" form, each at most once;
// anything else dies with exit 2. Non-flag tokens are returned as the
// positional arguments; the caller decides whether they are allowed.
func parseJobFlags(sub string, args []string) (vals map[string]string, bare map[string]bool, pos []string) {
	spec := jobFlagSpec[sub]
	valued := map[string]bool{}
	for _, flag := range spec.valued {
		valued[flag] = true
	}
	vals = map[string]string{}
	bare = map[string]bool{}
	for i := 0; i < len(args); i++ {
		token := args[i]
		name, value, hasValue := strings.Cut(token, "=")
		if !strings.HasPrefix(name, "--") {
			pos = append(pos, token)
			continue
		}
		if hasValue {
			if !valued[name] {
				platform.Die("job "+sub+": unknown flag '"+name+"'", 2)
			}
			if _, seen := vals[name]; seen {
				platform.Die("job "+sub+": repeated flag "+name, 2)
			}
			vals[name] = value
			continue
		}
		if _, isBare := bare[name]; isBare || !valued[name] {
			if !flagIn(spec.bare, name) {
				platform.Die("job "+sub+": unknown flag '"+name+"'", 2)
			}
			bare[name] = true
			continue
		}
		if _, seen := vals[name]; seen {
			platform.Die("job "+sub+": repeated flag "+name, 2)
		}
		if i+1 >= len(args) {
			platform.Die("job "+sub+": missing value for "+name, 2)
		}
		i++
		vals[name] = args[i]
	}
	return vals, bare, pos
}

func flagIn(list []string, flag string) bool {
	for _, item := range list {
		if item == flag {
			return true
		}
	}
	return false
}

// jobRequiredID returns a validated --id or dies with exit 2 (job.Dir's
// contract regex, plus the "." / ".." refusal).
func jobRequiredID(sub string, vals map[string]string) string {
	id, ok := vals["--id"]
	if !ok {
		platform.Die("job "+sub+": missing --id", 2)
	}
	if _, err := job.Dir("", id); err != nil {
		platform.Die(err.Error(), 2)
	}
	return id
}

// jobMSValue returns a bounded integer flag (0..600000) or its fallback.
func jobMSValue(sub, flag string, vals map[string]string, fallback int) int {
	raw, ok := vals[flag]
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > jobWaitBoundMS {
		platform.Die("job "+sub+": "+flag+" must be an integer from 0 to 600000", 2)
	}
	return n
}

// jobSinceValue returns the events --since flag (integer >= 0).
func jobSinceValue(sub string, vals map[string]string) int {
	raw, ok := vals["--since"]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		platform.Die("job "+sub+": --since must be an integer of at least 0", 2)
	}
	return n
}

// allJobStores opens a job store for every checkout <job_repos_root>/*/* on
// this machine, resolving each checkout's state root read-only.
func allJobStores(env platform.Env) ([]*job.Store, error) {
	machine, err := job.LoadMachine(platform.Current(), env)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(machine.ReposRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stores []*job.Store
	for _, org := range entries {
		if !org.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(machine.ReposRoot, org.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			checkout := filepath.Join(machine.ReposRoot, org.Name(), repo.Name())
			cfg := core.LoadConfig(env, checkout)
			stores = append(stores, job.Open(core.StateRootPath(&cfg, env, checkout)))
		}
	}
	return stores, nil
}

// locateJob resolves the store that holds jobs/<id>/state.json across every
// checkout. None holds it and the error is nil (the caller prints the
// not_found line and exits 3); more than one is exit 2.
func locateJob(env platform.Env, id string) (*job.Store, error) {
	if _, err := job.Dir("", id); err != nil {
		return nil, err
	}
	stores, err := allJobStores(env)
	if err != nil {
		return nil, err
	}
	var hit *job.Store
	for _, store := range stores {
		if _, err := os.Stat(filepath.Join(store.Root, "jobs", id, "state.json")); err == nil {
			if hit != nil {
				return nil, &platform.ExitError{Code: 2, Msg: "job: id " + id + " exists in more than one checkout"}
			}
			hit = store
		}
	}
	return hit, nil
}

// dieJob maps a *job.ExitError or *platform.ExitError to platform.Die; any
// other error dies with exit 2 and its message.
func dieJob(sub string, err error) {
	var jobExit *job.ExitError
	if errors.As(err, &jobExit) && jobExit != nil {
		platform.Die(jobExit.Msg, jobExit.Code)
	}
	var platformExit *platform.ExitError
	if errors.As(err, &platformExit) && platformExit != nil {
		platform.Die(platformExit.Msg, platformExit.Code)
	}
	platform.Die("job "+sub+": "+err.Error(), 2)
}

// jobHasCode reports whether err is a *job.ExitError with the given code.
func jobHasCode(err error, code int) bool {
	var jobExit *job.ExitError
	return errors.As(err, &jobExit) && jobExit != nil && jobExit.Code == code
}

func printJobNotFound() {
	_, _ = fmt.Fprintln(platform.Stdout, `{"status":"not_found"}`)
}

// jobStatusLine is the status/wait JSON line (contract field order).
type jobStatusLine struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	Motivo  *string `json:"motivo"`
	Eventos struct {
		Total     int `json:"total"`
		UltimoSeq int `json:"ultimo_seq"`
	} `json:"eventos"`
	DecisionsAckedSeq  int   `json:"decisions_acked_seq"`
	DecisionsPendentes []int `json:"decisions_pendentes"`
}

func printJobStatusLine(snap job.Snapshot) {
	line := jobStatusLine{
		ID:                 snap.State.ID,
		Status:             snap.State.Status,
		Motivo:             snap.State.Motivo,
		DecisionsAckedSeq:  snap.State.DecisionsAckedSeq,
		DecisionsPendentes: snap.Pending,
	}
	line.Eventos.Total = snap.EventTotal
	line.Eventos.UltimoSeq = snap.LastSeq
	if line.DecisionsPendentes == nil {
		line.DecisionsPendentes = []int{}
	}
	raw, err := json.Marshal(line)
	if err != nil {
		platform.Die("job: cannot render status: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(raw))
}

// jobSnapshotOrNotFound locates the job and returns its snapshot; an unknown
// id prints the contract's not_found line and returns 3.
func jobSnapshotOrNotFound(sub, id string, env platform.Env) (job.Snapshot, int) {
	store, err := locateJob(env, id)
	if err != nil {
		dieJob(sub, err)
		return job.Snapshot{}, 0
	}
	if store == nil {
		printJobNotFound()
		return job.Snapshot{}, 3
	}
	snap, err := store.Snapshot(id)
	if err != nil {
		if jobHasCode(err, job.ExitNotFound) {
			printJobNotFound()
			return job.Snapshot{}, 3
		}
		dieJob(sub, err)
		return job.Snapshot{}, 0
	}
	return snap, 0
}

// jobStoreOrNotFound locates the job store; an unknown id prints the
// not_found line and returns (nil, 3).
func jobStoreOrNotFound(sub, id string, env platform.Env) (*job.Store, int) {
	store, err := locateJob(env, id)
	if err != nil {
		dieJob(sub, err)
		return nil, 0
	}
	if store == nil {
		printJobNotFound()
		return nil, 3
	}
	return store, 0
}

func jobStatus(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("status", args)
	if len(pos) != 0 {
		platform.Die("job status: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("status", vals)
	snap, code := jobSnapshotOrNotFound("status", id, env)
	if code != 0 {
		return code
	}
	printJobStatusLine(snap)
	return 0
}

func jobWait(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("wait", args)
	if len(pos) != 0 {
		platform.Die("job wait: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("wait", vals)
	bound := jobMSValue("wait", "--timeout", vals, jobWaitBoundMS)
	store, code := jobStoreOrNotFound("wait", id, env)
	if code != 0 {
		return code
	}
	var waitErr error
	if _, waitErr = store.Wait(id, time.Duration(bound)*time.Millisecond); waitErr != nil && !jobHasCode(waitErr, job.ExitTimeout) {
		dieJob("wait", waitErr)
	}
	snap, err := store.Snapshot(id)
	if err != nil {
		dieJob("wait", err)
	}
	printJobStatusLine(snap)
	if jobHasCode(waitErr, job.ExitTimeout) {
		return job.ExitTimeout
	}
	return jobWaitExit(snap)
}

// jobWaitExit maps the state a wait ended in to the contract exit code;
// collected/closed use the stored report's terminal outcome (0 when no report).
func jobWaitExit(snap job.Snapshot) int {
	switch snap.State.Status {
	case job.StatusCollected, job.StatusClosed:
		if snap.Report == nil {
			return 0
		}
		return jobOutcomeExit(snap.Report.Status)
	default:
		return jobOutcomeExit(snap.State.Status)
	}
}

func jobOutcomeExit(status string) int {
	switch status {
	case job.StatusDone:
		return 0
	case job.StatusBlocked:
		return job.ExitBlocked
	case job.StatusTimeout:
		return job.ExitTimeout
	case job.StatusFailed:
		return job.ExitFailed
	case job.StatusCanceled:
		return job.ExitCanceled
	default:
		return 0
	}
}

func jobEvents(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("events", args)
	if len(pos) != 0 {
		platform.Die("job events: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("events", vals)
	since := jobSinceValue("events", vals)
	bound := jobMSValue("events", "--wait", vals, 0)
	store, code := jobStoreOrNotFound("events", id, env)
	if code != 0 {
		return code
	}
	page, err := store.Events(id, since, time.Duration(bound)*time.Millisecond)
	if err != nil {
		dieJob("events", err)
	}
	for _, line := range page.Lines() {
		_, _ = fmt.Fprintln(platform.Stdout, line)
	}
	return 0
}

func jobCollect(args []string, env platform.Env) int {
	vals, bare, pos := parseJobFlags("collect", args)
	if len(pos) != 0 {
		platform.Die("job collect: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("collect", vals)
	store, code := jobStoreOrNotFound("collect", id, env)
	if code != 0 {
		return code
	}
	snap, err := store.Snapshot(id)
	if err != nil {
		dieJob("collect", err)
	}
	dir, err := job.Dir(store.Root, id)
	if err != nil {
		dieJob("collect", err)
	}
	if bare["--md"] {
		data, err := os.ReadFile(filepath.Join(dir, "report.md"))
		if err != nil {
			if os.IsNotExist(err) {
				platform.Die("job collect: report.md not found", 4)
			}
			dieJob("collect", err)
		}
		_, _ = platform.Stdout.Write(data)
		return 0
	}
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		if os.IsNotExist(err) {
			printJobCollectMissing(snap)
			return 0
		}
		dieJob("collect", err)
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		platform.Die("job collect: report.json unreadable: "+err.Error(), 2)
	}
	// A body of JSON null unmarshals into a nil map without error; refuse it
	// before the assignment instead of panicking. The file is not changed.
	if report == nil {
		platform.Die("job collect: report.json unreadable: expected an object", 2)
	}
	report["decisions_pendentes"] = snap.Pending
	line, err := json.Marshal(report)
	if err != nil {
		platform.Die("job collect: cannot render report.json: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(line))
	if !bare["--verify"] {
		return 0
	}
	return jobVerifyArtefatos(dir, raw)
}

func printJobCollectMissing(snap job.Snapshot) {
	line := struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Report any    `json:"report"`
	}{ID: snap.State.ID, Status: snap.State.Status}
	raw, err := json.Marshal(line)
	if err != nil {
		platform.Die("job collect: cannot render status: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(raw))
}

// jobVerifyArtefatos recomputes the SHA-256 of every artefatos[].path
// (relative paths are relative to the job directory) and exits 16 when one
// differs or is missing.
func jobVerifyArtefatos(dir string, raw []byte) int {
	var doc struct {
		Artefatos []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"artefatos"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		platform.Die("job collect: report.json unreadable: "+err.Error(), 2)
	}
	for _, artefato := range doc.Artefatos {
		path := artefato.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			platform.Die("job collect: artefato "+artefato.Path+" changed or missing", 16)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != artefato.SHA256 {
			platform.Die("job collect: artefato "+artefato.Path+" changed or missing", 16)
		}
	}
	return 0
}

// jobReadBody reads a body from the file named path, or from stdin when
// path is "-"; at most cap+1 bytes are read and going over the cap dies
// with exit 2.
func jobReadBody(sub, what, path string, cap int, limit string) []byte {
	if path == "-" {
		data, err := io.ReadAll(io.LimitReader(jobStdin, int64(cap)+1))
		if err != nil {
			platform.Die("job "+sub+": cannot read "+what+" from stdin: "+err.Error(), 2)
		}
		if len(data) > cap {
			platform.Die("job "+sub+": "+what+" exceeds "+limit, 2)
		}
		return data
	}
	f, err := os.Open(path)
	if err != nil {
		platform.Die("job "+sub+": cannot read "+what+" from "+path+": "+err.Error(), 2)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(cap)+1))
	if err != nil {
		platform.Die("job "+sub+": cannot read "+what+" from "+path+": "+err.Error(), 2)
	}
	if len(data) > cap {
		platform.Die("job "+sub+": "+what+" exceeds "+limit, 2)
	}
	return data
}

// jobAck acknowledges the decisions up to --upto (a no-op when lower or
// equal) and prints the values after the call, from a fresh snapshot.
func jobAck(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("ack", args)
	if len(pos) != 0 {
		platform.Die("job ack: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("ack", vals)
	raw, ok := vals["--upto"]
	if !ok {
		platform.Die("job ack: missing --upto", 2)
	}
	upto, err := strconv.Atoi(raw)
	if err != nil || upto < 1 {
		platform.Die("job ack: --upto must be an integer of at least 1", 2)
	}
	store, code := jobStoreOrNotFound("ack", id, env)
	if code != 0 {
		return code
	}
	if _, err := store.Ack(id, upto); err != nil {
		dieJob("ack", err)
	}
	snap, err := store.Snapshot(id)
	if err != nil {
		dieJob("ack", err)
	}
	pending := snap.Pending
	if pending == nil {
		pending = []int{}
	}
	line, err := json.Marshal(struct {
		ID                 string `json:"id"`
		DecisionsAckedSeq  int    `json:"decisions_acked_seq"`
		DecisionsPendentes []int  `json:"decisions_pendentes"`
	}{ID: snap.State.ID, DecisionsAckedSeq: snap.State.DecisionsAckedSeq, DecisionsPendentes: pending})
	if err != nil {
		platform.Die("job ack: cannot render ack: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(line))
	return 0
}

// jobClose moves a terminal or collected job to closed. Pending decisions
// without --force print the state with the seqs listed and exit 24; close
// is state-only in this phase (no Herdr, process or workspace release).
func jobClose(args []string, env platform.Env) int {
	vals, bare, pos := parseJobFlags("close", args)
	if len(pos) != 0 {
		platform.Die("job close: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("close", vals)
	store, code := jobStoreOrNotFound("close", id, env)
	if code != 0 {
		return code
	}
	if _, err := store.Close(id, bare["--force"]); err != nil {
		if jobHasCode(err, job.ExitUnacked) {
			snap, snapErr := store.Snapshot(id)
			if snapErr != nil {
				dieJob("close", snapErr)
			}
			printJobCloseLine(snap.State.ID, snap.State.Status, snap.Pending)
			return job.ExitUnacked
		}
		dieJob("close", err)
	}
	snap, err := store.Snapshot(id)
	if err != nil {
		dieJob("close", err)
	}
	printJobCloseLine(snap.State.ID, snap.State.Status, snap.Pending)
	return 0
}

// printJobCloseLine is the close JSON line (contract field order).
func printJobCloseLine(id, status string, pending []int) {
	if pending == nil {
		pending = []int{}
	}
	line, err := json.Marshal(struct {
		ID                 string `json:"id"`
		Status             string `json:"status"`
		DecisionsPendentes []int  `json:"decisions_pendentes"`
	}{ID: id, Status: status, DecisionsPendentes: pending})
	if err != nil {
		platform.Die("job close: cannot render close: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(line))
}

// jobParseRefs splits the comma-separated --refs items into key=value pairs;
// an item without "=" or with an empty key is exit 2. Unknown keys are
// refused by the package.
func jobParseRefs(sub string, vals map[string]string) map[string]string {
	raw, ok := vals["--refs"]
	if !ok {
		return nil
	}
	refs := map[string]string{}
	for _, item := range strings.Split(raw, ",") {
		key, value, hasEq := strings.Cut(item, "=")
		if !hasEq || key == "" {
			platform.Die("job "+sub+": --refs items must be key=value pairs", 2)
		}
		refs[key] = value
	}
	return refs
}

// jobNote appends one orchestrator event and prints the stored event as one
// JSON line. The escopo/decision rules stay in the package (appendEvent).
func jobNote(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("note", args)
	id := jobRequiredID("note", vals)
	tipo, ok := vals["--tipo"]
	if !ok {
		platform.Die("job note: missing --tipo", 2)
	}
	if !jobNoteTypes[tipo] {
		platform.Die("job note: --tipo must be checkpoint, question, decision, or note", 2)
	}
	if len(pos) != 1 {
		platform.Die("job note: expected exactly one summary argument (or -)", 2)
	}
	var resumo string
	if pos[0] == "-" {
		resumo = string(jobReadBody("note", "summary", "-", jobNoteBodyLimit, "16 KiB"))
	} else {
		resumo = pos[0]
	}
	refs := jobParseRefs("note", vals)
	store, code := jobStoreOrNotFound("note", id, env)
	if code != 0 {
		return code
	}
	event, err := store.Note(id, job.EventIn{Tipo: tipo, Resumo: resumo, Escopo: vals["--escopo"], Motivo: vals["--motivo"], Refs: refs})
	if err != nil {
		dieJob("note", err)
	}
	line, err := json.Marshal(event)
	if err != nil {
		platform.Die("job note: cannot render event: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(line))
	return 0
}

// jobRefusePhase2 validates the supervise inputs and refuses: the supervisor
// arrives in the next slice.
//
// TODO(DJA-194): phase 2 — supervise.
func jobRefusePhase2(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("supervise", args)
	if len(pos) > 1 {
		platform.Die("job supervise: unexpected argument '"+pos[1]+"'", 2)
	}
	id := jobRequiredID("supervise", vals)
	_, code := jobStoreOrNotFound("supervise", id, env)
	if code != 0 {
		return code
	}
	platform.Die("job supervise: not available yet", 2)
	return 0
}

// jobControlStatus prints the fresh snapshot status line after a control
// request: the store and id are known, so only a read failure dies.
func jobControlStatus(sub string, store *job.Store, id string) {
	snap, err := store.Snapshot(id)
	if err != nil {
		dieJob(sub, err)
	}
	printJobStatusLine(snap)
}

// jobAmend queues the amendment body (a file or -) as a control request and
// prints the status line. The supervisor delivers it later and appends
// amend_received; no event is written here and no Herdr call is made.
func jobAmend(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("amend", args)
	if len(pos) > 1 {
		platform.Die("job amend: unexpected argument '"+pos[1]+"'", 2)
	}
	id := jobRequiredID("amend", vals)
	if len(pos) != 1 {
		platform.Die("job amend: missing body (a file or -)", 2)
	}
	store, code := jobStoreOrNotFound("amend", id, env)
	if code != 0 {
		return code
	}
	body := jobReadBody("amend", "body", pos[0], jobAmendBodyLimit, "64 KiB")
	if _, err := store.RequestAmend(id, body); err != nil {
		dieJob("amend", err)
	}
	jobControlStatus("amend", store, id)
	return 0
}

// jobSend queues the note body (a file or -) as a control request and prints
// the status line. Like amend, it writes no event and makes no Herdr call.
func jobSend(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("send", args)
	if len(pos) > 1 {
		platform.Die("job send: unexpected argument '"+pos[1]+"'", 2)
	}
	id := jobRequiredID("send", vals)
	if len(pos) != 1 {
		platform.Die("job send: missing body (a file or -)", 2)
	}
	store, code := jobStoreOrNotFound("send", id, env)
	if code != 0 {
		return code
	}
	body := jobReadBody("send", "body", pos[0], jobSendBodyLimit, "16 KiB")
	if _, err := store.RequestSend(id, body); err != nil {
		dieJob("send", err)
	}
	jobControlStatus("send", store, id)
	return 0
}

// jobCancel queues the control cancel request; the default grace is 120
// seconds when --grace is absent. A no-op cancel still prints the status
// line and exits 0.
func jobCancel(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("cancel", args)
	if len(pos) > 1 {
		platform.Die("job cancel: unexpected argument '"+pos[1]+"'", 2)
	}
	id := jobRequiredID("cancel", vals)
	grace := 120
	if raw, ok := vals["--grace"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 3600 {
			platform.Die("job cancel: --grace must be an integer from 0 to 3600", 2)
		}
		grace = n
	}
	store, code := jobStoreOrNotFound("cancel", id, env)
	if code != 0 {
		return code
	}
	if _, err := store.RequestCancel(id, grace); err != nil {
		dieJob("cancel", err)
	}
	jobControlStatus("cancel", store, id)
	return 0
}

// jobCheckpoint queues the immediate-push marker and prints the status line.
// A no-op checkpoint still exits 0.
func jobCheckpoint(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("checkpoint", args)
	if len(pos) > 1 {
		platform.Die("job checkpoint: unexpected argument '"+pos[1]+"'", 2)
	}
	id := jobRequiredID("checkpoint", vals)
	store, code := jobStoreOrNotFound("checkpoint", id, env)
	if code != 0 {
		return code
	}
	if _, err := store.RequestCheckpoint(id); err != nil {
		dieJob("checkpoint", err)
	}
	jobControlStatus("checkpoint", store, id)
	return 0
}

// jobStart validates the start inputs in the contract order and, with
// --dry-run, prints the resolved plan without writing any job state; without
// it, it refuses after the same validation (phase 2 prepares the job).
func jobStart(args []string, env platform.Env, cwd string) int {
	vals, bare, pos := parseJobFlags("start", args)
	if len(pos) != 0 {
		platform.Die("job start: unexpected argument '"+pos[0]+"'", 2)
	}
	id := jobRequiredID("start", vals)
	repo, ok := vals["--repo"]
	if !ok {
		platform.Die("job start: missing --repo", 2)
	}
	if !jobRepoPattern.MatchString(repo) {
		platform.Die("job start: invalid --repo '"+repo+"'", 2)
	}
	org := repo[:strings.IndexByte(repo, '/')]
	repoName := repo[strings.IndexByte(repo, '/')+1:]
	base := ""
	if raw, ok := vals["--base"]; ok {
		if !jobBasePattern.MatchString(raw) || strings.Contains(raw, "..") {
			platform.Die("job start: invalid --base '"+raw+"'", 2)
		}
		base = raw
	}
	modoFlag := ""
	if raw, ok := vals["--mode"]; ok {
		if raw != "worktree" && raw != "workspace" {
			platform.Die("job start: invalid --mode '"+raw+"'", 2)
		}
		modoFlag = raw
	}
	timeout := 0
	if raw, ok := vals["--timeout"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1440 {
			platform.Die("job start: --timeout must be an integer from 1 to 1440", 2)
		}
		timeout = n
	}
	briefPath, ok := vals["--brief"]
	if !ok {
		platform.Die("job start: missing --brief", 2)
	}
	raw := jobReadBody("start", "brief", briefPath, job.MaxBriefBytes, "256 KiB")
	info, err := job.ValidateBrief(raw, id)
	if err != nil {
		dieJob("start", err)
	}
	if info.Repo != repo {
		platform.Die("job start: brief repo '"+info.Repo+"' does not match --repo '"+repo+"'", 2)
	}
	machine, err := job.LoadMachine(platform.Current(), env)
	if err != nil {
		dieJob("start", err)
	}
	if err := machine.AllowsOrg(org); err != nil {
		dieJob("start", err)
	}
	if err := machine.LabelMatches(info.Maquina); err != nil {
		dieJob("start", err)
	}
	checkout := filepath.Join(machine.ReposRoot, org, repoName)
	team, err := job.ResolveTeam(platform.Current(), env, checkout, org, repoName, info.Equipe)
	if err != nil {
		dieJob("start", err)
	}
	// The rendered brief lints against the checkout config when the checkout
	// exists, else the cwd config; the temp file is removed before returning.
	tmp, err := os.CreateTemp("", "herdr-soho-job-brief-*.md")
	if err != nil {
		platform.Die("job start: cannot create temp brief: "+err.Error(), 2)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(info.Markdown); err != nil {
		tmp.Close()
		platform.Die("job start: cannot write temp brief: "+err.Error(), 2)
	}
	if err := tmp.Close(); err != nil {
		platform.Die("job start: cannot write temp brief: "+err.Error(), 2)
	}
	cfg := core.LoadConfig(env, cwd)
	if st, statErr := os.Stat(checkout); statErr == nil && st.IsDir() {
		cfg = core.LoadConfig(env, checkout)
	}
	if err := job.LintBrief(tmpPath, &cfg, env); err != nil {
		dieJob("start", err)
	}
	if timeout == 0 {
		timeout = machine.TimeoutMin
	}
	if !bare["--dry-run"] {
		// TODO(DJA-194): phase 2 — prepare, create the workspace and supervise.
		platform.Die("job start: only --dry-run is available in this build", 2)
	}
	resolvedBase := info.Base
	if base != "" {
		resolvedBase = base
	}
	var baseOut *string
	if resolvedBase != "" {
		baseOut = &resolvedBase
	}
	modo := "worktree"
	if info.Modo != "" {
		modo = info.Modo
	}
	if modoFlag != "" {
		modo = modoFlag
	}
	override := team.OverrideKeys
	if override == nil {
		override = []string{}
	}
	line, err := json.Marshal(struct {
		Status     string  `json:"status"`
		ID         string  `json:"id"`
		Repo       string  `json:"repo"`
		Base       *string `json:"base"`
		Modo       string  `json:"modo"`
		TimeoutMin int     `json:"timeout_min"`
		BriefSHA   string  `json:"brief_sha256"`
		Equipe     struct {
			Fonte         string   `json:"fonte"`
			OverrideBrief []string `json:"override_brief"`
		} `json:"equipe"`
	}{
		Status: "dry_run", ID: id, Repo: repo, Base: baseOut, Modo: modo,
		TimeoutMin: timeout, BriefSHA: info.Hash,
		Equipe: struct {
			Fonte         string   `json:"fonte"`
			OverrideBrief []string `json:"override_brief"`
		}{Fonte: team.Source, OverrideBrief: override},
	})
	if err != nil {
		platform.Die("job start: cannot render dry run: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(line))
	return 0
}

func jobList(args []string, env platform.Env) int {
	vals, _, pos := parseJobFlags("list", args)
	if len(pos) != 0 {
		platform.Die("job list: unexpected argument '"+pos[0]+"'", 2)
	}
	filter := ""
	if raw, ok := vals["--state"]; ok {
		if !jobLifecycleStates[raw] {
			platform.Die("job list: invalid --state '"+raw+"'", 2)
		}
		filter = raw
	}
	stores, err := allJobStores(env)
	if err != nil {
		dieJob("list", err)
	}
	counts := map[string]int{}
	total := 0
	for _, store := range stores {
		entries, err := os.ReadDir(filepath.Join(store.Root, "jobs"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			dieJob("list", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			snap, err := store.Snapshot(entry.Name())
			if err != nil {
				continue
			}
			if filter != "" && snap.State.Status != filter {
				continue
			}
			counts[snap.State.Status]++
			total++
		}
	}
	line, err := json.Marshal(struct {
		Total     int            `json:"total"`
		PorEstado map[string]int `json:"por_estado"`
	}{Total: total, PorEstado: counts})
	if err != nil {
		platform.Die("job list: cannot render counts: "+err.Error(), 2)
	}
	_, _ = fmt.Fprintln(platform.Stdout, string(line))
	return 0
}

// jobReadSubs are the read-only job subcommands; every other subcommand
// is a writing one and never passes the NOWRITE allowlist.
var jobReadSubs = map[string]bool{
	"status": true, "wait": true, "events": true, "collect": true, "list": true,
}

// nowriteJobRead accepts the read-only job invocations by shape (values are
// not validated here): status/wait/events/collect with --id and their flags,
// list with an optional --state.
func nowriteJobRead(argv []string) bool {
	if len(argv) == 0 || !jobReadSubs[argv[0]] {
		return false
	}
	spec := jobFlagSpec[argv[0]]
	valued := map[string]bool{}
	for _, flag := range spec.valued {
		valued[flag] = true
	}
	seen := map[string]bool{}
	sawID := false
	for i := 1; i < len(argv); i++ {
		token := argv[i]
		name, value, hasValue := strings.Cut(token, "=")
		if !strings.HasPrefix(name, "--") {
			return false
		}
		if hasValue {
			if !valued[name] || value == "" || strings.HasPrefix(value, "-") {
				return false
			}
		} else if valued[name] {
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") {
				return false
			}
			i++
		} else if !flagIn(spec.bare, name) {
			return false
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		if name == "--id" {
			sawID = true
		}
	}
	if argv[0] == "list" {
		return true
	}
	return sawID
}
