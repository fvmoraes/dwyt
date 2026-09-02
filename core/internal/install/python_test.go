package install

import (
	"errors"
	"strings"
	"testing"
)

func TestWindowsPythonCandidatesPreferPy312(t *testing.T) {
	candidates := pythonCandidatesForOS("windows")
	if len(candidates) == 0 {
		t.Fatal("expected Windows Python candidates")
	}
	if got := candidates[0]; got.bin != "py" || len(got.prefixArgs) != 1 || got.prefixArgs[0] != "-3.12" {
		t.Fatalf("first Windows candidate = %#v, want py -3.12", got)
	}

	var probed []pythonCommand
	python, compatible, err := lookupCompatiblePythonCandidates(
		candidates,
		func(name string) (string, error) {
			if name == "py" {
				return `C:\Windows\py.exe`, nil
			}
			return "", errors.New("not installed")
		},
		func(string) bool { return false },
		func(command pythonCommand) (int, int, bool) {
			probed = append(probed, command)
			return 3, 12, true
		},
		func(pythonCommand) error { return nil },
	)
	if err != nil {
		t.Fatalf("lookupCompatiblePythonCandidates: %v", err)
	}
	if !compatible {
		t.Fatal("expected Python 3.12 to be considered compatible")
	}
	if python.bin != `C:\Windows\py.exe` || len(python.prefixArgs) != 1 || python.prefixArgs[0] != "-3.12" {
		t.Fatalf(`selected command = %#v, want C:\Windows\py.exe -3.12`, python)
	}
	if len(probed) != 1 || probed[0].prefixArgs[0] != "-3.12" {
		t.Fatalf("version probe = %#v, want py -3.12", probed)
	}
	if got := strings.Join(python.commandArgs("-m", "venv", `C:\dwyt\headroom-venv`), " "); got != `-3.12 -m venv C:\dwyt\headroom-venv` {
		t.Fatalf("venv command args = %q, want py selector before -m venv", got)
	}
}

func TestLookupCompatiblePythonRejectsNewerVersionForHeadroomBootstrap(t *testing.T) {
	validated := false
	_, compatible, err := lookupCompatiblePythonCandidates(
		[]pythonCommand{{bin: "python"}},
		func(string) (string, error) { return "/mock/python", nil },
		func(string) bool { return false },
		func(pythonCommand) (int, int, bool) { return 3, 13, true },
		func(pythonCommand) error {
			validated = true
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "Python 3.13 não suportado") {
		t.Fatalf("expected unsupported Python 3.13 error, got %v", err)
	}
	if compatible {
		t.Fatal("Python 3.13 must not block automatic installation of Python 3.12")
	}
	if validated {
		t.Fatal("pre-flight must not run for an unsupported Python version")
	}
}

func TestHeadroomPythonVersionRange(t *testing.T) {
	for _, tc := range []struct {
		name         string
		major, minor int
		wantErr      bool
	}{
		{name: "lowest supported", major: 3, minor: 10},
		{name: "highest supported", major: 3, minor: 12},
		{name: "too old", major: 3, minor: 9, wantErr: true},
		{name: "too new", major: 3, minor: 13, wantErr: true},
		{name: "wrong major", major: 4, minor: 0, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePythonVersion(tc.major, tc.minor)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validatePythonVersion(%d, %d) error = %v, wantErr %v", tc.major, tc.minor, err, tc.wantErr)
			}
		})
	}
}
