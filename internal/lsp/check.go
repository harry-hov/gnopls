package lsp

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gnolang/gno/gnovm/pkg/gnomod"
	"github.com/harry-hov/gnopls/internal/env"
	"go.uber.org/multierr"
)

type FileInfo struct {
	Name string
	Body string
}

type PackageInfo struct {
	Dir, ImportPath string
	Files           []*FileInfo
}

type PackageGetter interface {
	GetPackageInfo(path string) *PackageInfo
}

// GetPackageInfo accepts path(abs) or importpath and returns
// PackageInfo if found.
// Note: it doesn't work for relative path
func GetPackageInfo(path string) (*PackageInfo, error) {
	// if not absolute, assume its import path
	if !filepath.IsAbs(path) {
		if env.GlobalEnv.GNOROOT == "" {
			// if GNOROOT is unknown, we can't locate the
			// `examples` and `stdlibs`
			return nil, errors.New("GNOROOT not set")
		}
		if strings.HasPrefix(path, "gno.land/") { // look in `examples`
			path = filepath.Join(env.GlobalEnv.GNOROOT, "examples", path)
		} else { // look into `stdlibs`
			path = filepath.Join(env.GlobalEnv.GNOROOT, "gnovm", "stdlibs", path)
		}
	}
	return getPackageInfo(path)
}

func getPackageInfo(path string) (*PackageInfo, error) {
	filenames, err := ListGnoFiles(path)
	if err != nil {
		return nil, err
	}
	var importpath string
	gm, gmErr := gnomod.ParseAt(path)
	if gmErr != nil {
		importpath = "" // TODO
	} else {
		importpath = gm.Module.Mod.Path
	}
	files := []*FileInfo{}
	for _, fname := range filenames {
		if strings.HasSuffix(fname, "_test.gno") ||
			strings.HasSuffix(fname, "_filetest.gno") {
			continue
		}
		absPath, err := filepath.Abs(fname)
		if err != nil {
			return nil, err
		}
		bsrc, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		text := string(bsrc)
		files = append(files, &FileInfo{Name: filepath.Base(fname), Body: text})
	}
	return &PackageInfo{
		ImportPath: importpath,
		Dir:        path,
		Files:      files,
	}, nil
}

type TypeCheck struct {
	cache map[string]*TypeCheckResult
	cfg   *types.Config
}

func NewTypeCheck() (*TypeCheck, *error) {
	var errs error
	return &TypeCheck{
		cache: map[string]*TypeCheckResult{},
		cfg: &types.Config{
			Error: func(err error) {
				errs = multierr.Append(errs, err)
			},
		},
	}, &errs
}

func (tc *TypeCheck) Import(path string) (*types.Package, error) {
	return tc.ImportFrom(path, "", 0)
}

// ImportFrom returns the imported package for the given import path
func (tc *TypeCheck) ImportFrom(path, _ string, _ types.ImportMode) (*types.Package, error) {
	if pkg, ok := tc.cache[path]; ok {
		return pkg.pkg, pkg.err
	}
	pkg, err := GetPackageInfo(path)
	if err != nil {
		err := fmt.Errorf("package %q not found", path)
		tc.cache[path] = &TypeCheckResult{err: err}
		return nil, err
	}
	res := pkg.TypeCheck(tc)
	tc.cache[path] = res
	return res.pkg, res.err
}

func (pi *PackageInfo) TypeCheck(tc *TypeCheck) *TypeCheckResult {
	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	files := make([]*ast.File, 0, len(pi.Files))
	var errs error
	for _, f := range pi.Files {
		if !strings.HasSuffix(f.Name, ".gno") ||
			strings.HasSuffix(f.Name, "_filetest.gno") ||
			strings.HasSuffix(f.Name, "_test.gno") {
			continue
		}

		pgf, err := parser.ParseFile(fset, f.Name, f.Body, parser.ParseComments|parser.DeclarationErrors|parser.SkipObjectResolution)
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}

		files = append(files, pgf)
	}
	pkg, err := tc.cfg.Check(pi.ImportPath, fset, files, info)
	return &TypeCheckResult{pkg: pkg, fset: fset, files: files, info: info, err: err}
}

type TypeCheckResult struct {
	pkg   *types.Package
	fset  *token.FileSet
	files []*ast.File
	info  *types.Info
	err   error
}

func (tcr *TypeCheckResult) Errors() []ErrorInfo {
	errs := multierr.Errors(tcr.err)
	res := make([]ErrorInfo, 0, len(errs))
	for _, err := range errs {
		parts := strings.Split(err.Error(), ":")
		if len(parts) < 4 {
			slog.Error("TYPECHECK", "skipped", err)
		}
		filename := strings.TrimSpace(parts[0])
		line, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		col, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		msg := strings.TrimSpace(strings.Join(parts[3:], ":"))
		res = append(res, ErrorInfo{
			FileName: filename,
			Line:     line,
			Column:   col,
			Span:     []int{col, math.MaxInt},
			Msg:      msg,
			Tool:     "go/typecheck",
		})
	}
	return res
}
