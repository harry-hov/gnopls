package tools

import (
	"os/exec"
	"path/filepath"
)

// Build a Gno package: gno transpile -gobuild <dir>.
// TODO: Remove this in the favour of directly using tools/transpile.go
func Build(rootDir string) ([]byte, error) {
	return exec.Command("gno", "transpile", "-skip-imports", "-gobuild", filepath.Join(rootDir)).CombinedOutput()
}
