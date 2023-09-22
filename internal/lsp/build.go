package lsp

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/harry-hov/gnopls/internal/tools"
)

type ErrorInfo struct {
	FileName string
	Line     int
	Column   int
	Span     []int
	Msg      string
	Tool     string
}

func (s *server) PrecompileAndBuild(file *GnoFile) ([]ErrorInfo, error) {
	pkgDir := filepath.Dir(file.URI.Filename())
	pkgName := filepath.Base(pkgDir)
	tmpDir := filepath.Join(s.env.GNOHOME, "gnopls", "tmp", pkgName)

	err := copyDir(pkgDir, tmpDir)
	if err != nil {
		return nil, err
	}

	preOut, _ := tools.Precompile(tmpDir)
	slog.Info(string(preOut))
	if len(preOut) > 0 {
		return parseErrors(file, string(preOut), "precompile")
	}

	buildOut, _ := tools.Build(tmpDir)
	slog.Info(string(buildOut))
	return parseErrors(file, string(buildOut), "build")
}

// This is used to extract information from the `gno build` command
// (see `parseError` below).
//
// TODO: Maybe there's a way to get this in a structured format?
var errorRe = regexp.MustCompile(`(?m)^([^#]+?):(\d+):(\d+):(.+)$`)

// parseErrors parses the output of the `gno build` command for errors.
//
// They look something like this:
//
// ```
// command-line-arguments
// # command-line-arguments
// <file>:20:9: undefined: strin
//
// <pkg_path>: build pkg: std go compiler: exit status 1
//
// 1 go build errors
// ```
func parseErrors(file *GnoFile, output, cmd string) ([]ErrorInfo, error) {
	errors := []ErrorInfo{}

	matches := errorRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return errors, nil
	}

	for _, match := range matches {
		line, err := strconv.Atoi(match[2])
		if err != nil {
			return nil, err
		}

		column, err := strconv.Atoi(match[3])
		if err != nil {
			return nil, err
		}
		slog.Info("parsing", "line", line, "column", column, "msg", match[4])

		errorInfo := findError(file, match[1], line, column, match[4], cmd)
		errors = append(errors, errorInfo)
	}

	return errors, nil
}

// findError finds the error in the document, shifting the line and column
// numbers to account for the header information in the generated Go file.
func findError(file *GnoFile, fname string, line, col int, msg string, tool string) ErrorInfo {
	msg = strings.TrimSpace(msg)
	if tool == "precompile" {
		// fname parsed from precompile result can be incorrect
		// e.g filename = `filename.gno: precompile: parse: tmp.gno`
		parts := strings.Split(fname, ":")
		fname = parts[0]
	}

	// Error messages are of the form:
	//
	// <token> <error> (<info>)
	// <error>: <token>
	//
	// We want to strip the parens and find the token in the file.
	parens := regexp.MustCompile(`\((.+)\)`)
	needle := parens.ReplaceAllString(msg, "")
	tokens := strings.Fields(needle)

	shiftedLine := line
	if tool == "build" {
		// The generated Go file has 4 lines of header information.
		//
		// +1 for zero-indexing.
		shiftedLine = line - 4
	}

	errorInfo := ErrorInfo{
		FileName: strings.TrimPrefix(GoToGnoFileName(filepath.Base(fname)), "."),
		Line:     shiftedLine,
		Column:   col,
		Span:     []int{0, 0},
		Msg:      msg,
		Tool:     tool,
	}

	lines := strings.SplitAfter(string(file.Src), "\n")
	for i, l := range lines {
		if i != shiftedLine-1 { // zero-indexed
			continue
		}
		for _, token := range tokens {
			tokRe := regexp.MustCompile(fmt.Sprintf(`\b%s\b`, regexp.QuoteMeta(token)))
			if tokRe.MatchString(l) {
				errorInfo.Line = i + 1
				errorInfo.Span = []int{col, col + len(token)}
				return errorInfo
			}
		}
	}

	// If we couldn't find the token, just return the original error + the
	// full line.
	errorInfo.Span = []int{col, col + 1}

	return errorInfo
}
