package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeSystem answers LookPath and Output from fixed values.
type fakeSystem struct {
	path      string
	lookErr   error
	output    string
	outputErr error
}

func (f fakeSystem) LookPath(string) (string, error) { return f.path, f.lookErr }

func (f fakeSystem) Output(context.Context, string, ...string) ([]byte, error) {
	return []byte(f.output), f.outputErr
}

func TestRun(t *testing.T) {
	podman := "/opt/homebrew/bin/podman"
	tests := []struct {
		name       string
		args       []string
		sys        fakeSystem
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "no args", args: nil, wantCode: exitUsage, wantStderr: "Usage: s1s"},
		{name: "unknown command", args: []string{"nope"}, wantCode: exitUsage, wantStderr: `unknown command "nope"`},
		{name: "help", args: []string{"help"}, wantCode: exitOK, wantStdout: "Commands:"},
		{name: "version", args: []string{"version"}, wantCode: exitOK, wantStdout: "s1s "},
		{
			name:       "prereq podman missing",
			args:       []string{"prereq"},
			sys:        fakeSystem{lookErr: errors.New("not found")},
			wantCode:   exitFail,
			wantStderr: "podman: not found",
		},
		{
			name:       "prereq list fails",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, outputErr: errors.New("exit status 125")},
			wantCode:   exitFail,
			wantStderr: "could not list machines",
		},
		{
			name:       "prereq bad json",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, output: "not json"},
			wantCode:   exitFail,
			wantStderr: "unexpected output",
		},
		{
			name:       "prereq no machine",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, output: "[]"},
			wantCode:   exitFail,
			wantStderr: "podman machine init",
		},
		{
			name:       "prereq machine stopped",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, output: `[{"Name":"m1","Default":true,"Running":false}]`},
			wantCode:   exitFail,
			wantStderr: "m1 is stopped. Run: podman machine start m1",
		},
		{
			name:       "prereq machine starting",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, output: `[{"Name":"m1","Default":true,"Starting":true}]`},
			wantCode:   exitFail,
			wantStderr: "still starting",
		},
		{
			name:       "prereq machine running",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, output: `[{"Name":"m1","Default":true,"Running":true}]`},
			wantCode:   exitOK,
			wantStdout: "m1 is running",
		},
		{
			name:       "prereq prefers the running machine over a stopped default",
			args:       []string{"prereq"},
			sys:        fakeSystem{path: podman, output: `[{"Name":"d","Default":true},{"Name":"r","Running":true}]`},
			wantCode:   exitOK,
			wantStdout: "r is running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tt.args, &stdout, &stderr, tt.sys, Options{})

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
