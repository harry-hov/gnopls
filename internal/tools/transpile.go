package tools

import (
	"os/exec"
	"path/filepath"
)

// Transpile a Gno package: gno transpile <dir>.
func Transpile(rootDir string) ([]byte, error) {
	return exec.Command("gno", "transpile", "-skip-imports", filepath.Join(rootDir)).CombinedOutput()
}
