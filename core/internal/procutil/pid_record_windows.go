//go:build windows

package procutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func securePIDFile(file *os.File) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if user == nil || user.User.Sid == nil {
		return errors.New("current process token has no user SID")
	}

	// Windows file modes do not restrict the DACL. Install a protected DACL
	// granting full control only to the current user, which is the native
	// equivalent of publishing a user-private 0600 PID record.
	descriptor, err := windows.SecurityDescriptorFromString(
		fmt.Sprintf("D:P(A;;FA;;;%s)", user.User.Sid.String()),
	)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("private PID record DACL is empty")
	}
	return windows.SetNamedSecurityInfo(
		file.Name(),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

func lockPIDRecord(dir, name string) (func() error, error) {
	path := filepath.Join(dir, "."+name+".pid.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := securePIDFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}

	overlapped := &windows.Overlapped{}
	handle := windows.Handle(file.Fd())
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error {
		return errors.Join(
			windows.UnlockFileEx(handle, 0, 1, 0, overlapped),
			file.Close(),
		)
	}, nil
}

// readPIDFile opens a PID record with delete sharing so a concurrent atomic
// replacement can complete while the reader consumes its complete version.
func readPIDFile(path string) ([]byte, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("create PID record reader")
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}

func replacePIDFile(source, target string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}

	// A concurrent reader can briefly deny replacement even after the file is
	// opened with delete sharing: MoveFileEx reports that conflict as either a
	// sharing violation or access denied, depending on the Windows filesystem.
	// Retrying preserves atomic publication once the reader closes; a persistent
	// access error is still returned to the caller.
	var moveErr error
	const attempts = 20
	for attempt := 0; attempt < attempts; attempt++ {
		moveErr = windows.MoveFileEx(
			sourcePtr,
			targetPtr,
			windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
		)
		if moveErr == nil || (!errors.Is(moveErr, windows.ERROR_SHARING_VIOLATION) && !errors.Is(moveErr, windows.ERROR_ACCESS_DENIED)) {
			return moveErr
		}
		if attempt+1 < attempts {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return moveErr
}
