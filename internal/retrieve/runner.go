package retrieve

import (
	"os/exec"
)

// Runner abstracts external command execution so passes can be unit-tested
// with canned fixtures instead of shelling out for real.
//
// Contract: Run returns (stdout, stderr, err). A non-zero exit is reported
// via err; callers treat any error as "this source produced no results"
// and continue. Failures never propagate to the CLI's exit code.
type Runner interface {
	Run(name string, args ...string) (stdout []byte, stderr []byte, err error)
}

// ExecRunner is the default production implementation backed by os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	stdout, err := cmd.Output()
	var stderr []byte
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	return stdout, stderr, err
}

// FakeRunner is a test double. Responses are keyed by the stringified
// arg list, allowing tests to precisely script external command behaviour.
type FakeRunner struct {
	// Responses maps "<name> <arg1> <arg2> ..." to the canned stdout.
	// Unknown calls return an error (exit 1 equivalent).
	Responses map[string]FakeResponse
	Calls     []string
}

type FakeResponse struct {
	Stdout []byte
	Err    error
}

func (f *FakeRunner) Run(name string, args ...string) ([]byte, []byte, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	f.Calls = append(f.Calls, key)
	if f.Responses == nil {
		return nil, nil, errNotFound{key}
	}
	r, ok := f.Responses[key]
	if !ok {
		return nil, nil, errNotFound{key}
	}
	return r.Stdout, nil, r.Err
}

type errNotFound struct{ key string }

func (e errNotFound) Error() string { return "fake runner: no response for: " + e.key }
