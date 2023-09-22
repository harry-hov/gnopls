package lsp

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.lsp.dev/protocol"
)

func ListGnoPackages(paths []string) ([]string, error) {
	res := []string{}
	for _, path := range paths {
		visited := map[string]bool{}
		err := filepath.WalkDir(path, func(curpath string, f fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("%s: walk dir: %w", path, err)
			}
			if f.IsDir() {
				return nil // skip
			}
			if filepath.Ext(curpath) != ".gno" {
				return nil
			}
			parentDir := filepath.Dir(curpath)
			if _, found := visited[parentDir]; found {
				return nil
			}
			visited[parentDir] = true
			res = append(res, parentDir)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

func ListGnoFiles(path string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".gno") {
			continue
		}
		files = append(files, filepath.Join(path, e.Name()))
	}
	return files, nil
}

// GoToGnoFileName return gno file name from generated go file
// If not a generated go file, return unchanged fname
func GoToGnoFileName(fname string) string {
	fname = strings.TrimSuffix(fname, ".gen_test.go")
	fname = strings.TrimSuffix(fname, ".gen.go")
	return fname
}

// copyDir copies the content of src to dst (not the src dir itself),
// the paths have to be absolute to ensure consistent behavior.
func copyDir(src, dst string) error {
	if !filepath.IsAbs(src) || !filepath.IsAbs(dst) {
		return fmt.Errorf("src or dst path not absolute, src: %s dst: %s", src, dst)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot read dir: %s", src)
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: '%s', error: '%w'", dst, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.Type().IsDir() {
			copyDir(srcPath, dstPath)
		} else if entry.Type().IsRegular() {
			copyFile(srcPath, dstPath)
		}
	}

	return nil
}

// copyFile copies the file from src to dst, the paths have
// to be absolute to ensure consistent behavior.
func copyFile(src, dst string) error {
	if !filepath.IsAbs(src) || !filepath.IsAbs(dst) {
		return fmt.Errorf("src or dst path not absolute, src: %s dst: %s", src, dst)
	}

	// verify if it's regular flile
	srcStat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("cannot copy file: %w", err)
	}
	if !srcStat.Mode().IsRegular() {
		return fmt.Errorf("%s not a regular file", src)
	}

	// create dst file
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// open src file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// copy srcFile -> dstFile
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}

func posToRange(line int, span []int) *protocol.Range {
	return &protocol.Range{
		Start: protocol.Position{
			Line:      uint32(line - 1),
			Character: uint32(span[0] - 1),
		},
		End: protocol.Position{
			Line:      uint32(line - 1),
			Character: uint32(span[1] - 1),
		},
	}
}

func symbolToKind(symbol string) protocol.CompletionItemKind {
	switch symbol {
	case "const":
		return protocol.CompletionItemKindConstant
	case "func":
		return protocol.CompletionItemKindFunction
	case "type":
		return protocol.CompletionItemKindClass
	case "var":
		return protocol.CompletionItemKindVariable
	case "struct":
		return protocol.CompletionItemKindStruct
	case "interface":
		return protocol.CompletionItemKindInterface
	case "package":
		return protocol.CompletionItemKindModule
	default:
		return protocol.CompletionItemKindValue
	}
}
