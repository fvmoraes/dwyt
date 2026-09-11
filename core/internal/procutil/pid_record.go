package procutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const pidRecordVersion = 1

var (
	errProcessNotFound             = errors.New("process not found")
	errProcessChangedDuringInspect = errors.New("process changed during inspection")
	errMalformedPIDRecord          = errors.New("malformed pid record")
	errUnsupportedPIDRecord        = errors.New("unsupported pid record version")
	errLegacyPIDRecord             = errors.New("legacy pid record is not authorized to terminate")
	errPIDRecordChanged            = errors.New("pid record changed while stopping")
)

type processIdentity struct {
	PID                int
	ExecutableIdentity string
	StartTimeToken     string
}

type processInspector func(pid int) (processIdentity, error)
type processTerminator func(pid int) error

type pidRecord struct {
	Version            int    `json:"version"`
	PID                int    `json:"pid"`
	ExecutableIdentity string `json:"executable_identity"`
	StartTimeToken     string `json:"start_time_token"`
}

type pidRecordFormat uint8

const (
	pidRecordFormatCurrent pidRecordFormat = iota + 1
	pidRecordFormatLegacy
)

// StopStatus classifies the outcome for one tracked PID record.
type StopStatus string

const (
	StopStatusStopped             StopStatus = "stopped"
	StopStatusStaleRemoved        StopStatus = "stale_removed"
	StopStatusLegacyUnauthorized  StopStatus = "legacy_unauthorized"
	StopStatusMalformedRecord     StopStatus = "malformed_record"
	StopStatusUnsupportedVersion  StopStatus = "unsupported_version"
	StopStatusUnverifiable        StopStatus = "unverifiable"
	StopStatusExecutableMismatch  StopStatus = "executable_mismatch"
	StopStatusPIDReused           StopStatus = "pid_reused"
	StopStatusTerminateFailed     StopStatus = "terminate_failed"
	StopStatusRecordCleanupFailed StopStatus = "record_cleanup_failed"
)

// StopResult reports the authorization and termination outcome for one PID
// record. Validated means both executable identity and start-time token
// matched immediately before the terminator call.
type StopResult struct {
	Name       string     `json:"name"`
	PID        int        `json:"pid,omitempty"`
	Status     StopStatus `json:"status"`
	Validated  bool       `json:"validated,omitempty"`
	Signaled   bool       `json:"signaled,omitempty"`
	Terminated bool       `json:"terminated,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// StopReport contains deterministic, daemon-last results. Signaled contains
// names for which the terminator was invoked; Stopped contains the subset for
// which it returned success. Errors mirrors the returned aggregate error in a
// serialization-friendly form.
type StopReport struct {
	Results  []StopResult `json:"results"`
	Signaled []string     `json:"signaled,omitempty"`
	Stopped  []string     `json:"stopped,omitempty"`
	Errors   []string     `json:"errors,omitempty"`
}

// StopAllTrackedWithReport securely validates and stops every tracked process.
// Per-record failures are classified in the report and joined in the returned
// error. A missing run directory is an empty successful report.
func StopAllTrackedWithReport(dwytHome string) (StopReport, error) {
	return stopAllTrackedWith(dwytHome, inspectProcess, TerminateTree)
}

func writePIDWithInspector(dwytHome, name string, pid int, inspect processInspector) error {
	if err := validatePIDName(name); err != nil {
		return err
	}
	if pid <= 0 {
		return fmt.Errorf("write pid record %q: invalid pid %d", name, pid)
	}
	if inspect == nil {
		return fmt.Errorf("write pid record %q: nil process inspector", name)
	}

	dir := PIDDir(dwytHome)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pid record directory: %w", err)
	}
	unlock, err := lockPIDRecord(dir, name)
	if err != nil {
		return fmt.Errorf("lock pid record %q: %w", name, err)
	}
	defer func() { _ = unlock() }()

	identity, err := inspect(pid)
	if err != nil {
		return fmt.Errorf("inspect process %d for pid record %q: %w", pid, name, err)
	}
	if err := validateProcessIdentity(pid, identity); err != nil {
		return fmt.Errorf("inspect process %d for pid record %q: %w", pid, name, err)
	}

	record := pidRecord{
		Version:            pidRecordVersion,
		PID:                pid,
		ExecutableIdentity: identity.ExecutableIdentity,
		StartTimeToken:     identity.StartTimeToken,
	}
	return writePIDRecordAtomicLocked(dir, name, record)
}

func validatePIDName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("invalid pid record name %q", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid pid record name %q", name)
	}
	return nil
}

func validateProcessIdentity(pid int, identity processIdentity) error {
	if identity.PID != pid {
		return fmt.Errorf("inspector returned pid %d for pid %d", identity.PID, pid)
	}
	if strings.TrimSpace(identity.ExecutableIdentity) == "" {
		return errors.New("inspector returned an empty executable identity")
	}
	if strings.TrimSpace(identity.StartTimeToken) == "" {
		return errors.New("inspector returned an empty start-time token")
	}
	return nil
}

func validatePIDRecord(record pidRecord) error {
	if record.Version != pidRecordVersion {
		return fmt.Errorf("version is %d, want %d", record.Version, pidRecordVersion)
	}
	if record.PID <= 0 {
		return fmt.Errorf("pid must be positive, got %d", record.PID)
	}
	if strings.TrimSpace(record.ExecutableIdentity) == "" {
		return errors.New("executable identity is empty")
	}
	if strings.TrimSpace(record.StartTimeToken) == "" {
		return errors.New("start-time token is empty")
	}
	return nil
}

func writePIDRecordAtomic(dir, name string, record pidRecord) error {
	if err := validatePIDName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pid record directory: %w", err)
	}
	unlock, err := lockPIDRecord(dir, name)
	if err != nil {
		return fmt.Errorf("lock pid record %q: %w", name, err)
	}
	defer func() { _ = unlock() }()
	return writePIDRecordAtomicLocked(dir, name, record)
}

func writePIDRecordAtomicLocked(dir, name string, record pidRecord) error {
	if err := validatePIDRecord(record); err != nil {
		return fmt.Errorf("write pid record %q: %w", name, err)
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal pid record %q: %w", name, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "."+name+".pid-*")
	if err != nil {
		return fmt.Errorf("create temporary pid record %q: %w", name, err)
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	if err := securePIDFile(tmp); err != nil {
		return fmt.Errorf("secure temporary pid record %q: %w", name, err)
	}
	written, err := tmp.Write(data)
	if err != nil {
		return fmt.Errorf("write temporary pid record %q: %w", name, err)
	}
	if written != len(data) {
		return fmt.Errorf("write temporary pid record %q: %w", name, io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary pid record %q: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary pid record %q: %w", name, err)
	}
	closed = true

	target := filepath.Join(dir, name+".pid")
	if err := replacePIDFile(tmpPath, target); err != nil {
		return fmt.Errorf("replace pid record %q: %w", name, err)
	}
	return nil
}

func readPIDRecordFile(path string, requireRegular bool) (pidRecord, pidRecordFormat, []byte, error) {
	if requireRegular {
		info, err := os.Lstat(path)
		if err != nil {
			return pidRecord{}, 0, nil, err
		}
		if !info.Mode().IsRegular() {
			return pidRecord{}, 0, nil, fmt.Errorf("%w: record is not a regular file", errMalformedPIDRecord)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return pidRecord{}, 0, nil, err
	}
	record, format, err := parsePIDRecord(data)
	if err != nil {
		return pidRecord{}, 0, data, err
	}
	return record, format, data, nil
}

func parsePIDRecord(data []byte) (pidRecord, pidRecordFormat, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return pidRecord{}, 0, fmt.Errorf("%w: empty record", errMalformedPIDRecord)
	}

	if pid, err := strconv.Atoi(string(trimmed)); err == nil {
		return pidRecord{PID: pid}, pidRecordFormatLegacy, nil
	}
	if trimmed[0] != '{' {
		return pidRecord{}, 0, fmt.Errorf("%w: expected a JSON object or decimal pid", errMalformedPIDRecord)
	}

	var header struct {
		Version *int `json:"version"`
	}
	if err := json.Unmarshal(trimmed, &header); err != nil {
		return pidRecord{}, 0, fmt.Errorf("%w: %v", errMalformedPIDRecord, err)
	}
	if header.Version == nil {
		return pidRecord{}, 0, fmt.Errorf("%w: version is missing", errMalformedPIDRecord)
	}
	if *header.Version != pidRecordVersion {
		return pidRecord{}, 0, fmt.Errorf("%w: got %d, want %d", errUnsupportedPIDRecord, *header.Version, pidRecordVersion)
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var record pidRecord
	if err := decoder.Decode(&record); err != nil {
		return pidRecord{}, 0, fmt.Errorf("%w: %v", errMalformedPIDRecord, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return pidRecord{}, 0, fmt.Errorf("%w: trailing JSON content", errMalformedPIDRecord)
	}
	if err := validatePIDRecord(record); err != nil {
		return pidRecord{}, 0, fmt.Errorf("%w: %v", errMalformedPIDRecord, err)
	}
	return record, pidRecordFormatCurrent, nil
}

func stopAllTrackedWith(dwytHome string, inspect processInspector, terminate processTerminator) (StopReport, error) {
	var report StopReport
	if inspect == nil {
		err := errors.New("nil process inspector")
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}
	if terminate == nil {
		err := errors.New("nil process terminator")
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}

	dir := PIDDir(dwytHome)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		err = fmt.Errorf("list tracked pid records: %w", err)
		report.Errors = append(report.Errors, err.Error())
		return report, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".pid"))
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "daemon" {
			return false
		}
		if names[j] == "daemon" {
			return true
		}
		return names[i] < names[j]
	})

	var resultErrors []error
	for _, name := range names {
		result, resultErr := stopTrackedPIDRecord(dir, name, inspect, terminate)
		report.Results = append(report.Results, result)
		if result.Signaled {
			report.Signaled = append(report.Signaled, name)
		}
		if result.Terminated {
			report.Stopped = append(report.Stopped, name)
		}
		if resultErr != nil {
			report.Errors = append(report.Errors, resultErr.Error())
			resultErrors = append(resultErrors, resultErr)
		}
	}
	return report, errors.Join(resultErrors...)
}

func stopTrackedPIDRecord(dir, name string, inspect processInspector, terminate processTerminator) (StopResult, error) {
	result := StopResult{Name: name}
	path := filepath.Join(dir, name+".pid")
	unlock, err := lockPIDRecord(dir, name)
	if err != nil {
		result.Status = StopStatusUnverifiable
		return stopFailure(result, fmt.Errorf("lock pid record %q: %w", name, err))
	}
	defer func() { _ = unlock() }()

	record, format, raw, err := readPIDRecordFile(path, true)
	if err != nil {
		switch {
		case errors.Is(err, errUnsupportedPIDRecord):
			result.Status = StopStatusUnsupportedVersion
		case errors.Is(err, errMalformedPIDRecord):
			result.Status = StopStatusMalformedRecord
		default:
			result.Status = StopStatusUnverifiable
		}
		return stopFailure(result, fmt.Errorf("read pid record %q: %w", name, err))
	}
	result.PID = record.PID

	if format == pidRecordFormatLegacy {
		result.Status = StopStatusLegacyUnauthorized
		return stopFailure(result, fmt.Errorf("pid record %q: %w", name, errLegacyPIDRecord))
	}

	current, err := inspect(record.PID)
	if errors.Is(err, errProcessNotFound) {
		if cleanupErr := removePIDRecordIfUnchanged(path, raw); cleanupErr != nil {
			result.Status = StopStatusRecordCleanupFailed
			return stopFailure(result, fmt.Errorf("remove dead stale pid record %q: %w", name, cleanupErr))
		}
		result.Status = StopStatusStaleRemoved
		return result, nil
	}
	if err != nil {
		result.Status = StopStatusUnverifiable
		return stopFailure(result, fmt.Errorf("inspect tracked process %q (pid %d): %w", name, record.PID, err))
	}
	if err := validateProcessIdentity(record.PID, current); err != nil {
		result.Status = StopStatusUnverifiable
		return stopFailure(result, fmt.Errorf("inspect tracked process %q (pid %d): %w", name, record.PID, err))
	}
	if current.StartTimeToken != record.StartTimeToken {
		result.Status = StopStatusPIDReused
		return stopFailure(result, fmt.Errorf("tracked process %q (pid %d): start-time token changed", name, record.PID))
	}
	if current.ExecutableIdentity != record.ExecutableIdentity {
		result.Status = StopStatusExecutableMismatch
		return stopFailure(result, fmt.Errorf("tracked process %q (pid %d): executable identity changed", name, record.PID))
	}

	// Keep this validation and call adjacent: no liveness probe or unrelated
	// work may reopen the PID-reuse window between authorization and signal.
	result.Validated = true
	result.Signaled = true
	if err := terminate(record.PID); err != nil {
		result.Status = StopStatusTerminateFailed
		return stopFailure(result, fmt.Errorf("terminate tracked process %q (pid %d): %w", name, record.PID, err))
	}
	result.Terminated = true

	if err := removePIDRecordIfUnchanged(path, raw); err != nil {
		result.Status = StopStatusRecordCleanupFailed
		return stopFailure(result, fmt.Errorf("remove stopped pid record %q: %w", name, err))
	}
	result.Status = StopStatusStopped
	return result, nil
}

func stopFailure(result StopResult, err error) (StopResult, error) {
	result.Error = err.Error()
	return result, err
}

func removePIDRecordIfUnchanged(path string, expected []byte) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return errPIDRecordChanged
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
