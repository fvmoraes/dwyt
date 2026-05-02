package deps

import (
	"fmt"
	"os/exec"

	"github.com/DeusData/core/internal/detect"
)

func Ensure(e *detect.Env) error {
	for _, dep := range []string{"curl", "git"} {
		if _, err := exec.LookPath(dep); err != nil {
			return fmt.Errorf("missing: %s", dep)
		}
		fmt.Printf("  ✓ %s ok\n", dep)
	}

	for _, py := range []string{"python3", "python"} {
		if _, err := exec.LookPath(py); err == nil {
			fmt.Printf("  ✓ python ok\n")
			break
		}
	}

	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node not found")
	}
	fmt.Printf("  ✓ node ok\n")

	return nil
}
