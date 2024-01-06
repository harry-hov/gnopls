package tools

import (
	"os/exec"
	"path/filepath"
)

// Build a Gno package: gno precompile -gobuild <dir>.
// TODO: Remove this in the favour of directly using tools/precompile.go
func Build(rootDir string) ([]byte, error) {
	return exec.Command("gno", "precompile", "-skip-imports", "-gobuild", filepath.Join(rootDir)).CombinedOutput()
}
