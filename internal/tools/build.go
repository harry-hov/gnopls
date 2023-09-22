package tools

import (
	"os/exec"
	"path/filepath"
)

// Build a Gno package: gno build <dir>.
func Build(rootDir string) ([]byte, error) {
	return exec.Command("gno", "build", filepath.Join(rootDir)).CombinedOutput()
}
