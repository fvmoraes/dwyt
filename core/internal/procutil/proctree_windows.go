//go:build windows

package procutil

import "context"

// TerminateTree uses taskkill /T through Terminate, which forcibly stops the
// target process and every descendant on Windows.
func TerminateTree(pid int) error {
	return TerminateTreeContext(context.Background(), pid)
}

func TerminateTreeContext(ctx context.Context, pid int) error {
	return TerminateContext(ctx, pid)
}
