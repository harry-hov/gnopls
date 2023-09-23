package tools

import (
	"os/exec"
	"path/filepath"
)

// Precompile a Gno package: gno precompile <dir>.
func Precompile(rootDir string) ([]byte, error) {
	return exec.Command("gno", "precompile", "-skip-imports", filepath.Join(rootDir)).CombinedOutput()
}
