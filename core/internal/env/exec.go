package env

import "os/exec"

func execRun(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
