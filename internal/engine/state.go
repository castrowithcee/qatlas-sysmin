package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Layout below the state directory: deploy/<project>/state.json and deploy/<project>/runs/<run id>.log.
const (
	deployDir   = "deploy"
	stateFile   = "state.json"
	runsDir     = "runs"
	dirMode     = 0o700
	fileMode    = 0o600
	stateSchema = 1
	// keepRuns bounds both the history in the state and the run log files of a project.
	keepRuns  = 50
	runIDForm = "20060102T150405.000000000Z"
)

var runLogRE = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z\.log$`)

// Actions of a run.
const (
	ActionAuto    = "auto"    // started by the planner, a trigger or RunNow
	ActionManual  = "manual"  // an update to versions a human chose
	ActionRestore = "restore" // data restored from a backup
)

// Results of a run.
const (
	ResultSucceeded      = "succeeded"
	ResultFailed         = "failed"
	ResultRolledBack     = "rolled-back"
	ResultRollbackFailed = "rollback-failed"
)

// State is what the engine keeps about one project. It holds versions, digests and redacted error texts,
// never a secret.
type State struct {
	Schema  int           `json:"schema"`
	Project string        `json:"project"`
	Sources []SourceState `json:"sources,omitempty"`
	Paused  bool          `json:"paused,omitempty"`
	// Fault is set when a rollback failed; the project stays paused until Resume clears both.
	Fault *Fault `json:"fault,omitempty"`
	Pin   *Pin   `json:"pin,omitempty"`
	// LastCheck is when the sources were last read.
	LastCheck time.Time `json:"last_check,omitzero"`
	// CheckError tells why the last run could not check or deploy, such as a missing approval.
	CheckError string `json:"check_error,omitempty"`
	// History holds the latest runs, oldest first.
	History []RunRecord `json:"history,omitempty"`
}

// SourceState is the state of one image, or of the git source with an empty Name.
type SourceState struct {
	Name           string `json:"name"`
	Current        string `json:"current,omitempty"`
	Digest         string `json:"digest,omitempty"`
	Previous       string `json:"previous,omitempty"`
	PreviousDigest string `json:"previous_digest,omitempty"`
	// Available lists the newer versions that are not deployed without a human, lowest first.
	Available []string `json:"available,omitempty"`
	// Reported lists the versions update.available was sent for.
	Reported []string `json:"reported,omitempty"`
	// Failed is a target that was rolled back; it is not deployed automatically again.
	Failed string `json:"failed,omitempty"`
	// Error is why the source could not be read in the last check.
	Error string `json:"error,omitempty"`
}

// Fault is the error state of a project whose rollback failed.
type Fault struct {
	Time       time.Time `json:"time"`
	Run        string    `json:"run"`
	FailedStep string    `json:"failed_step,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Pin holds a project on the versions it had when it was pinned.
type Pin struct {
	Time     time.Time         `json:"time"`
	Versions map[string]string `json:"versions,omitempty"`
}

// Change is the version change of one source in a run.
type Change struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// RunRecord is one finished run.
type RunRecord struct {
	ID       string        `json:"id"`
	Action   string        `json:"action"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration"`
	Result   string        `json:"result"`
	// ErrorCode is a fixed word naming the failure; empty on success.
	ErrorCode  string   `json:"error_code,omitempty"`
	FailedStep string   `json:"failed_step,omitempty"`
	Error      string   `json:"error,omitempty"`
	Changes    []Change `json:"changes,omitempty"`
	// Backup is the name of the backup the run made or restored.
	Backup string `json:"backup,omitempty"`
	// Log is the file name of the run log below the project's runs directory.
	Log string `json:"log,omitempty"`
}

func (s *State) source(name string) *SourceState {
	for i := range s.Sources {
		if s.Sources[i].Name == name {
			return &s.Sources[i]
		}
	}
	s.Sources = append(s.Sources, SourceState{Name: name})
	return &s.Sources[len(s.Sources)-1]
}

func (s *State) addRun(r RunRecord) {
	s.History = append(s.History, r)
	if n := len(s.History); n > keepRuns {
		s.History = append([]RunRecord(nil), s.History[n-keepRuns:]...)
	}
}

func (s State) clone() State {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	var out State
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

// store reads and writes the state files. Every change is a read-modify-write under one lock, so a pause
// set while a run is active is never overwritten by the run.
type store struct {
	dir string
	mu  sync.Mutex
}

func (s *store) projectDir(project string) string { return filepath.Join(s.dir, deployDir, project) }

func (s *store) load(project string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(project)
}

func (s *store) read(project string) (State, error) {
	data, err := os.ReadFile(filepath.Join(s.projectDir(project), stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return State{Schema: stateSchema, Project: project}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("cannot read the state of %s: %w", project, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil || st.Schema != stateSchema || st.Project != project {
		return State{}, fmt.Errorf("the state of %s is not readable", project)
	}
	return st, nil
}

// update applies fn to the stored state and writes the result atomically.
func (s *store) update(project string, fn func(*State)) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.read(project)
	if err != nil {
		return State{}, err
	}
	fn(&st)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return State{}, err
	}
	if err := s.write(project, append(data, '\n')); err != nil {
		return State{}, err
	}
	return st.clone(), nil
}

func (s *store) mkdirs(dirs ...string) error {
	for _, d := range dirs {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return err
		}
		if err := os.Chmod(d, dirMode); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) write(project string, data []byte) error {
	dir := s.projectDir(project)
	if err := s.mkdirs(filepath.Join(s.dir, deployDir), dir); err != nil {
		return fmt.Errorf("cannot write the state of %s: %w", project, err)
	}
	tmp, err := os.CreateTemp(dir, ".sysmin-state-*")
	if err != nil {
		return fmt.Errorf("cannot write the state of %s: %w", project, err)
	}
	name := tmp.Name()
	err = tmp.Chmod(fileMode)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(dir, stateFile))
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cannot write the state of %s: %w", project, err)
	}
	return nil
}

// createRunLog creates the log file of a new run and removes the oldest ones beyond keepRuns. The run id is
// the start time; a taken one moves on by a nanosecond.
func (s *store) createRunLog(project string, start time.Time) (*os.File, string, error) {
	dir := filepath.Join(s.projectDir(project), runsDir)
	if err := s.mkdirs(filepath.Join(s.dir, deployDir), s.projectDir(project), dir); err != nil {
		return nil, "", fmt.Errorf("cannot create the run log: %w", err)
	}
	t := start.UTC()
	for range 1000 {
		id := t.Format(runIDForm)
		f, err := os.OpenFile(filepath.Join(dir, id+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if err == nil {
			s.pruneRunLogs(dir)
			return f, id, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", fmt.Errorf("cannot create the run log: %w", err)
		}
		t = t.Add(time.Nanosecond)
	}
	return nil, "", errors.New("cannot create the run log: no free name")
}

func (s *store) pruneRunLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && runLogRE.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keepRuns {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// RunLogPath is the file of a run log; Log of a RunRecord names it.
func (e *Engine) RunLogPath(project, log string) (string, error) {
	if err := checkName(project); err != nil {
		return "", err
	}
	if !runLogRE.MatchString(log) {
		return "", fmt.Errorf("%q is not a run log", log)
	}
	return filepath.Join(e.store.projectDir(project), runsDir, log), nil
}
