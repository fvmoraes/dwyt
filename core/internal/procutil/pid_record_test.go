package procutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func testProcessIdentity(pid int, executable, startToken string) processIdentity {
	return processIdentity{
		PID:                pid,
		ExecutableIdentity: executable,
		StartTimeToken:     startToken,
	}
}

func writeTestPIDRecord(t *testing.T, home, name string, identity processIdentity) {
	t.Helper()
	if err := writePIDWithInspector(home, name, identity.PID, func(pid int) (processIdentity, error) {
		if pid != identity.PID {
			return processIdentity{}, fmt.Errorf("unexpected pid %d", pid)
		}
		return identity, nil
	}); err != nil {
		t.Fatalf("write test pid record %q: %v", name, err)
	}
}

func pidRecordPath(home, name string) string {
	return filepath.Join(PIDDir(home), name+".pid")
}

func requireRecordPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pid record should remain at %s: %v", path, err)
	}
}

func requireRecordRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid record should be removed from %s, stat error = %v", path, err)
	}
}

func onlyStopResult(t *testing.T, report StopReport) StopResult {
	t.Helper()
	if len(report.Results) != 1 {
		t.Fatalf("report has %d results, want 1: %#v", len(report.Results), report)
	}
	return report.Results[0]
}

func TestInspectProcessUsesStableRealIdentity(t *testing.T) {
	pid := os.Getpid()
	first, err := inspectProcess(pid)
	if err != nil {
		t.Fatalf("inspect current process: %v", err)
	}
	second, err := inspectProcess(pid)
	if err != nil {
		t.Fatalf("inspect current process again: %v", err)
	}
	if err := validateProcessIdentity(pid, first); err != nil {
		t.Fatalf("first process identity is invalid: %v", err)
	}
	if first != second {
		t.Fatalf("current process identity is unstable: first=%#v second=%#v", first, second)
	}
}

func TestPIDRecordRoundTripAndAtomicReplace(t *testing.T) {
	home := t.TempDir()
	dir := PIDDir(home)
	path := pidRecordPath(home, "daemon")
	firstIdentity := testProcessIdentity(4242, "/opt/dwyt/first", "start-1")
	secondIdentity := testProcessIdentity(4242, "/opt/dwyt/second", "start-2")

	writeTestPIDRecord(t, home, "daemon", firstIdentity)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Readers run while complete records are repeatedly replaced at the target.
	// They must observe either complete version, never absence or partial JSON.
	done := make(chan struct{})
	readErr := make(chan error, 1)
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := readPIDFile(path)
			if err != nil {
				readErr <- fmt.Errorf("concurrent read: %w", err)
				return
			}
			record, format, err := parsePIDRecord(data)
			if err != nil {
				readErr <- fmt.Errorf("concurrent parse: %w", err)
				return
			}
			if format != pidRecordFormatCurrent || (record.ExecutableIdentity != firstIdentity.ExecutableIdentity && record.ExecutableIdentity != secondIdentity.ExecutableIdentity) {
				readErr <- fmt.Errorf("concurrent reader observed unexpected record: %#v", record)
				return
			}
		}
	}()

	for i := 0; i < 12; i++ {
		identity := firstIdentity
		if i%2 == 0 {
			identity = secondIdentity
		}
		record := pidRecord{
			Version:            pidRecordVersion,
			PID:                identity.PID,
			ExecutableIdentity: identity.ExecutableIdentity,
			StartTimeToken:     identity.StartTimeToken,
		}
		if err := writePIDRecordAtomic(dir, "daemon", record); err != nil {
			close(done)
			readers.Wait()
			t.Fatalf("atomic replacement %d: %v", i, err)
		}
	}
	writeTestPIDRecord(t, home, "daemon", secondIdentity)
	close(done)
	readers.Wait()
	select {
	case err := <-readErr:
		t.Fatal(err)
	default:
	}

	secondInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := secondInfo.Mode().Perm(); got != 0600 {
			t.Fatalf("replacement mode = %#o, want 0600", got)
		}
	}

	record, format, _, err := readPIDRecordFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if format != pidRecordFormatCurrent || record.Version != pidRecordVersion || record.PID != secondIdentity.PID || record.ExecutableIdentity != secondIdentity.ExecutableIdentity || record.StartTimeToken != secondIdentity.StartTimeToken {
		t.Fatalf("replacement record = %#v format=%d, want second v1 identity", record, format)
	}
	if got := ReadPID(path); got != secondIdentity.PID {
		t.Fatalf("ReadPID = %d, want %d", got, secondIdentity.PID)
	}
	if got := ListPIDs(home)["daemon"]; got != secondIdentity.PID {
		t.Fatalf("ListPIDs daemon = %d, want %d", got, secondIdentity.PID)
	}
	if temps, err := filepath.Glob(filepath.Join(dir, ".daemon.pid-*")); err != nil || len(temps) != 0 {
		t.Fatalf("temporary records after replace = %v, err=%v", temps, err)
	}
}

func TestStopAllTrackedValidatedMatch(t *testing.T) {
	home := t.TempDir()
	identity := testProcessIdentity(101, "/opt/dwyt/service", "start-101")
	writeTestPIDRecord(t, home, "service", identity)

	var events []string
	report, err := stopAllTrackedWith(home, func(pid int) (processIdentity, error) {
		events = append(events, "inspect")
		return identity, nil
	}, func(pid int) error {
		events = append(events, "terminate")
		if pid != identity.PID {
			t.Fatalf("terminate pid = %d, want %d", pid, identity.PID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stop validated record: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"inspect", "terminate"}) {
		t.Fatalf("validation/termination events = %v, want adjacent inspect, terminate", events)
	}
	result := onlyStopResult(t, report)
	if result.Status != StopStatusStopped || !result.Validated || !result.Signaled || !result.Terminated {
		t.Fatalf("validated result = %#v", result)
	}
	if !reflect.DeepEqual(report.Signaled, []string{"service"}) || !reflect.DeepEqual(report.Stopped, []string{"service"}) {
		t.Fatalf("validated report = %#v", report)
	}
	requireRecordRemoved(t, pidRecordPath(home, "service"))
}

func TestStopAllTrackedRemovesDeadStaleRecord(t *testing.T) {
	home := t.TempDir()
	identity := testProcessIdentity(102, "/opt/dwyt/service", "start-102")
	writeTestPIDRecord(t, home, "service", identity)
	terminateCalls := 0

	report, err := stopAllTrackedWith(home, func(int) (processIdentity, error) {
		return processIdentity{}, fmt.Errorf("lookup: %w", errProcessNotFound)
	}, func(int) error {
		terminateCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("dead stale record should be cleanly handled: %v", err)
	}
	if terminateCalls != 0 {
		t.Fatalf("terminator called %d times for dead process", terminateCalls)
	}
	if result := onlyStopResult(t, report); result.Status != StopStatusStaleRemoved || result.Signaled {
		t.Fatalf("dead result = %#v", result)
	}
	requireRecordRemoved(t, pidRecordPath(home, "service"))
}

func TestStopAllTrackedFailsClosedOnIdentityValidation(t *testing.T) {
	inspectFailure := errors.New("inspection unavailable")
	tests := []struct {
		name       string
		current    processIdentity
		inspectErr error
		wantStatus StopStatus
	}{
		{
			name:       "pid reused",
			current:    testProcessIdentity(103, "/opt/dwyt/service", "new-start"),
			wantStatus: StopStatusPIDReused,
		},
		{
			name:       "executable mismatch",
			current:    testProcessIdentity(103, "/opt/other/program", "start-103"),
			wantStatus: StopStatusExecutableMismatch,
		},
		{
			name:       "unverifiable",
			inspectErr: inspectFailure,
			wantStatus: StopStatusUnverifiable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			stored := testProcessIdentity(103, "/opt/dwyt/service", "start-103")
			writeTestPIDRecord(t, home, "service", stored)
			terminateCalls := 0
			report, err := stopAllTrackedWith(home, func(int) (processIdentity, error) {
				return test.current, test.inspectErr
			}, func(int) error {
				terminateCalls++
				return nil
			})
			if err == nil {
				t.Fatalf("fail-closed outcome returned nil error: %#v", report)
			}
			if test.inspectErr != nil && !errors.Is(err, inspectFailure) {
				t.Fatalf("error = %v, want wrapped inspection failure", err)
			}
			if terminateCalls != 0 {
				t.Fatalf("terminator called %d times", terminateCalls)
			}
			result := onlyStopResult(t, report)
			if result.Status != test.wantStatus || result.Validated || result.Signaled {
				t.Fatalf("fail-closed result = %#v, want status %q", result, test.wantStatus)
			}
			requireRecordPresent(t, pidRecordPath(home, "service"))
		})
	}
}

func TestLegacyPIDIsReadableButNeverAuthorized(t *testing.T) {
	home := t.TempDir()
	path := pidRecordPath(home, "legacy")
	if err := os.MkdirAll(PIDDir(home), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("321\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ReadPID(path); got != 321 {
		t.Fatalf("ReadPID legacy = %d, want 321", got)
	}
	if got := ListPIDs(home)["legacy"]; got != 321 {
		t.Fatalf("ListPIDs legacy = %d, want 321", got)
	}

	inspectCalls, terminateCalls := 0, 0
	report, err := stopAllTrackedWith(home, func(int) (processIdentity, error) {
		inspectCalls++
		return processIdentity{}, nil
	}, func(int) error {
		terminateCalls++
		return nil
	})
	if err == nil || !errors.Is(err, errLegacyPIDRecord) {
		t.Fatalf("legacy stop error = %v, want legacy authorization error", err)
	}
	if inspectCalls != 0 || terminateCalls != 0 {
		t.Fatalf("legacy record called inspector=%d terminator=%d", inspectCalls, terminateCalls)
	}
	if result := onlyStopResult(t, report); result.Status != StopStatusLegacyUnauthorized || result.Signaled {
		t.Fatalf("legacy result = %#v", result)
	}
	requireRecordPresent(t, path)

	// Preserve ReadPID's historical decimal conversion exactly; non-positive
	// values remain readable but ListPIDs filters them and stop never trusts
	// any decimal record.
	negativePath := pidRecordPath(home, "legacy-negative")
	if err := os.WriteFile(negativePath, []byte("-7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ReadPID(negativePath); got != -7 {
		t.Fatalf("ReadPID negative legacy = %d, want -7", got)
	}
	if _, ok := ListPIDs(home)["legacy-negative"]; ok {
		t.Fatalf("ListPIDs must omit non-positive legacy PIDs")
	}
}

func TestMalformedPIDRecordNeverAuthorizesTermination(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantStatus StopStatus
	}{
		{name: "truncated", content: `{"version":1,"pid":44`, wantStatus: StopStatusMalformedRecord},
		{name: "unsupported", content: `{"version":2,"pid":44,"executable_identity":"x","start_time_token":"y"}`, wantStatus: StopStatusUnsupportedVersion},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			path := pidRecordPath(home, "broken")
			if err := os.MkdirAll(PIDDir(home), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatal(err)
			}
			inspectCalls, terminateCalls := 0, 0
			report, err := stopAllTrackedWith(home, func(int) (processIdentity, error) {
				inspectCalls++
				return processIdentity{}, nil
			}, func(int) error {
				terminateCalls++
				return nil
			})
			if err == nil {
				t.Fatalf("malformed record returned nil error: %#v", report)
			}
			if inspectCalls != 0 || terminateCalls != 0 {
				t.Fatalf("malformed record called inspector=%d terminator=%d", inspectCalls, terminateCalls)
			}
			if result := onlyStopResult(t, report); result.Status != test.wantStatus || result.Signaled {
				t.Fatalf("malformed result = %#v, want status %q", result, test.wantStatus)
			}
			requireRecordPresent(t, path)
		})
	}
}

func TestStopAllTrackedPreservesRecordOnTerminateError(t *testing.T) {
	home := t.TempDir()
	identity := testProcessIdentity(104, "/opt/dwyt/service", "start-104")
	writeTestPIDRecord(t, home, "service", identity)
	terminateFailure := errors.New("terminate failed")
	terminateCalls := 0

	report, err := stopAllTrackedWith(home, func(int) (processIdentity, error) {
		return identity, nil
	}, func(int) error {
		terminateCalls++
		return terminateFailure
	})
	if !errors.Is(err, terminateFailure) {
		t.Fatalf("terminate error = %v, want wrapped sentinel", err)
	}
	if terminateCalls != 1 {
		t.Fatalf("terminator called %d times, want 1", terminateCalls)
	}
	result := onlyStopResult(t, report)
	if result.Status != StopStatusTerminateFailed || !result.Validated || !result.Signaled || result.Terminated {
		t.Fatalf("terminate failure result = %#v", result)
	}
	if !reflect.DeepEqual(report.Signaled, []string{"service"}) || len(report.Stopped) != 0 {
		t.Fatalf("terminate failure report = %#v", report)
	}
	requireRecordPresent(t, pidRecordPath(home, "service"))
}

func TestStopAllTrackedStopsDaemonLast(t *testing.T) {
	home := t.TempDir()
	identities := map[int]processIdentity{
		201: testProcessIdentity(201, "/opt/dwyt/zeta", "start-201"),
		202: testProcessIdentity(202, "/opt/dwyt/daemon", "start-202"),
		203: testProcessIdentity(203, "/opt/dwyt/alpha", "start-203"),
	}
	writeTestPIDRecord(t, home, "zeta", identities[201])
	writeTestPIDRecord(t, home, "daemon", identities[202])
	writeTestPIDRecord(t, home, "alpha", identities[203])

	var terminated []int
	report, err := stopAllTrackedWith(home, func(pid int) (processIdentity, error) {
		identity, ok := identities[pid]
		if !ok {
			return processIdentity{}, fmt.Errorf("unexpected pid %d", pid)
		}
		return identity, nil
	}, func(pid int) error {
		terminated = append(terminated, pid)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(terminated, []int{203, 201, 202}) {
		t.Fatalf("termination order = %v, want alpha, zeta, daemon", terminated)
	}
	if !reflect.DeepEqual(report.Signaled, []string{"alpha", "zeta", "daemon"}) || !reflect.DeepEqual(report.Stopped, report.Signaled) {
		t.Fatalf("daemon-last report = %#v", report)
	}
	var resultNames []string
	for _, result := range report.Results {
		resultNames = append(resultNames, result.Name)
	}
	if !reflect.DeepEqual(resultNames, []string{"alpha", "zeta", "daemon"}) {
		t.Fatalf("result order = %v, want daemon last", resultNames)
	}
}

func TestStopCleanupDoesNotDeleteConcurrentReplacement(t *testing.T) {
	home := t.TempDir()
	oldIdentity := testProcessIdentity(301, "/opt/dwyt/old", "start-old")
	newIdentity := testProcessIdentity(301, "/opt/dwyt/new", "start-new")
	writeTestPIDRecord(t, home, "service", oldIdentity)

	inspectEntered := make(chan struct{})
	releaseInspect := make(chan struct{})
	type stopOutcome struct {
		report StopReport
		err    error
	}
	stopDone := make(chan stopOutcome, 1)
	go func() {
		report, err := stopAllTrackedWith(home, func(int) (processIdentity, error) {
			close(inspectEntered)
			<-releaseInspect
			return oldIdentity, nil
		}, func(int) error { return nil })
		stopDone <- stopOutcome{report: report, err: err}
	}()
	<-inspectEntered // The stopper holds the cross-process record lock here.

	writerDone := make(chan error, 1)
	go func() {
		writerDone <- writePIDWithInspector(home, "service", newIdentity.PID, func(int) (processIdentity, error) {
			return newIdentity, nil
		})
	}()
	select {
	case err := <-writerDone:
		t.Fatalf("concurrent writer bypassed the record lock: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: publication waits until stop has removed the old record.
	}

	close(releaseInspect)
	outcome := <-stopDone
	if outcome.err != nil {
		t.Fatalf("stop old record: %v (report %#v)", outcome.err, outcome.report)
	}
	if err := <-writerDone; err != nil {
		t.Fatalf("publish replacement after stop: %v", err)
	}

	record, format, _, err := readPIDRecordFile(pidRecordPath(home, "service"), true)
	if err != nil {
		t.Fatalf("read concurrent replacement: %v", err)
	}
	if format != pidRecordFormatCurrent || record.ExecutableIdentity != newIdentity.ExecutableIdentity || record.StartTimeToken != newIdentity.StartTimeToken {
		t.Fatalf("concurrent replacement was lost: record=%#v format=%d", record, format)
	}
}
