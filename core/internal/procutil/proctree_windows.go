//go:build windows

package procutil

// TerminateTree uses taskkill /T through Terminate, which forcibly stops the
// target process and every descendant on Windows.
func TerminateTree(pid int) error {
	return Terminate(pid)
}
