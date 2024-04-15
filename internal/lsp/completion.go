package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/gnolang/gno/gnovm/pkg/gnomod"
	cmap "github.com/orcaman/concurrent-map/v2"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"golang.org/x/tools/go/ast/astutil"
)

type CompletionStore struct {
	time time.Time

	pkgs []*Package
}

func (cs *CompletionStore) lookupPkg(pkg string) *Package {
	for _, p := range cs.pkgs {
		if p.Name == pkg {
			return p
		}
	}
	return nil
}

func (cs *CompletionStore) lookupSymbol(pkg, symbol string) *Symbol {
	for _, p := range cs.pkgs {
		if p.Name == pkg {
			for _, s := range p.Symbols {
				if s.Name == symbol {
					return s
				}
			}
		}
	}
	return nil
}

func (cs *CompletionStore) lookupSymbolByImports(symbol string, imports []*ast.ImportSpec) *Symbol {
	for _, spec := range imports {
		value := spec.Path.Value

		value = value[1 : len(value)-1]                 // remove quotes
		value = value[strings.LastIndex(value, "/")+1:] // get last part

		s := cs.lookupSymbol(value, symbol)
		if s != nil {
			return s
		}
	}

	return nil
}

type Package struct {
	Name       string
	ImportPath string
	Symbols    []*Symbol

	Functions  []*Function
	Methods    cmap.ConcurrentMap[string, []*Method]
	Structures []*Structure

	TypeCheckResult *TypeCheckResult
}

type Symbol struct {
	Position  token.Position
	FileURI   uri.URI
	Name      string
	Doc       string
	Signature string
	Kind      string // should be enum
}

type Function struct {
	Position  token.Position
	FileURI   uri.URI
	Name      string
	Arguments []*Field
	Doc       string
	Signature string
	Kind      string
}

func (f *Function) IsExported() bool {
	if len(f.Name) == 0 {
		return false // empty string
	}
	return unicode.IsUpper([]rune(f.Name)[0])
}

type Method struct {
	Position  token.Position
	FileURI   uri.URI
	Name      string
	Arguments []*Field
	Doc       string
	Signature string
	Kind      string
}

func (f *Method) IsExported() bool {
	if len(f.Name) == 0 {
		return false // empty string
	}
	return unicode.IsUpper([]rune(f.Name)[0])
}

type Structure struct {
	Position token.Position
	FileURI  uri.URI
	Name     string
	Fields   []*Field
	Doc      string
	String   string
}

type Field struct {
	Position token.Position
	Name     string
	Kind     string
}

func (s Symbol) String() string {
	return fmt.Sprintf("```gno\n%s\n```\n\n%s", s.Signature, s.Doc)
}

// Code that returns completion items
// TODO: Move completion store populating logic (rest of the code)
// to better place.
//
// ------------------------------------------------------
// Start

func (s *server) Completion(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.CompletionParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI

	// Get snapshot of the current file
	file, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}
	// Try parsing current file
	pgf, err := file.ParseGno2(ctx)
	if err != nil {
		return reply(ctx, nil, errors.New("cannot parse gno file"))
	}

	// Calculate offset and line
	offset := file.PositionToOffset(params.Position)
	line := params.Position.Line + 1 // starts at 0, so adding 1

	// Don't show completion items for imports
	for _, spec := range pgf.File.Imports {
		if spec.Path.Pos() <= token.Pos(offset) && token.Pos(offset) <= spec.Path.End() {
			return reply(ctx, nil, nil)
		}
	}

	// Completion is based on what precedes the cursor.
	// Find the path to the position before pos.
	paths, _ := astutil.PathEnclosingInterval(pgf.File, token.Pos(offset-1), token.Pos(offset-1))
	if paths == nil {
		return reply(ctx, nil, nil)
	}

	// Debug
	// slog.Info("COMPLETION", "token", fmt.Sprintf("%s", paths[0]))

	// Load pkg from cache
	pkg, ok := s.cache.pkgs.Get(filepath.Dir(string(uri.Filename())))
	if !ok {
		return reply(ctx, nil, nil)
	}

	switch n := paths[0].(type) {
	case *ast.Ident:
		_, tv := getTypeAndValue(
			*pgf.Fset,
			pkg.TypeCheckResult.info, n.Name,
			int(line),
			offset,
		)
		if tv == nil || tv.Type == nil {
			return completionPackageIdent(ctx, s, reply, params, pgf, n, true)
		}

		typeStr := tv.Type.String()
		if typeStr == "invalid type" {
			return completionPackageIdent(ctx, s, reply, params, pgf, n, false)
		}

		m := mode(*tv)
		if m != "var" {
			return reply(ctx, nil, nil)
		}

		if strings.Contains(typeStr, pkg.ImportPath) { // local
			t := parseType(typeStr, pkg.ImportPath)
			methods, ok := pkg.Methods.Get(t)
			if !ok {
				return reply(ctx, nil, nil)
			}
			items := []protocol.CompletionItem{}
			for _, m := range methods {
				items = append(items, protocol.CompletionItem{
					Label:         m.Name,
					InsertText:    m.Name + "()",
					Kind:          protocol.CompletionItemKindFunction,
					Detail:        m.Signature,
					Documentation: m.Doc,
				})
			}
			return reply(ctx, items, nil)
		}
		// check imports
		for _, spec := range pgf.File.Imports {
			path := spec.Path.Value[1 : len(spec.Path.Value)-1]
			if strings.Contains(typeStr, path) {
				parts := strings.Split(path, "/")
				last := parts[len(parts)-1]
				pkg := s.completionStore.lookupPkg(last)
				if pkg == nil {
					break
				}
				t := parseType(typeStr, path)
				methods, ok := pkg.Methods.Get(t)
				if !ok {
					break
				}
				items := []protocol.CompletionItem{}
				for _, m := range methods {
					items = append(items, protocol.CompletionItem{
						Label:         m.Name,
						InsertText:    m.Name + "()",
						Kind:          protocol.CompletionItemKindFunction,
						Detail:        m.Signature,
						Documentation: m.Doc,
					})
				}
				return reply(ctx, items, nil)
			}
		}
		return reply(ctx, nil, nil)
	case *ast.CallExpr:
		_, tv := getTypeAndValue(
			*pgf.Fset,
			pkg.TypeCheckResult.info, types.ExprString(n),
			int(line),
			offset,
		)
		if tv == nil || tv.Type == nil {
			return reply(ctx, nil, nil)
		}
		typeStr := tv.Type.String()
		slog.Info(typeStr)
		if strings.Contains(typeStr, pkg.ImportPath) { // local
			t := parseType(typeStr, pkg.ImportPath)
			methods, ok := pkg.Methods.Get(t)
			if !ok {
				return reply(ctx, nil, nil)
			}
			items := []protocol.CompletionItem{}
			for _, m := range methods {
				items = append(items, protocol.CompletionItem{
					Label:         m.Name,
					InsertText:    m.Name + "()",
					Kind:          protocol.CompletionItemKindFunction,
					Detail:        m.Signature,
					Documentation: m.Doc,
				})
			}
			return reply(ctx, items, nil)
		}
		// check imports
		for _, spec := range pgf.File.Imports {
			path := spec.Path.Value[1 : len(spec.Path.Value)-1]
			if strings.Contains(typeStr, path) {
				parts := strings.Split(path, "/")
				last := parts[len(parts)-1]
				pkg := s.completionStore.lookupPkg(last)
				if pkg == nil {
					break
				}
				t := parseType(typeStr, path)
				methods, ok := pkg.Methods.Get(t)
				if !ok {
					break
				}
				items := []protocol.CompletionItem{}
				for _, m := range methods {
					items = append(items, protocol.CompletionItem{
						Label:         m.Name,
						InsertText:    m.Name + "()",
						Kind:          protocol.CompletionItemKindFunction,
						Detail:        m.Signature,
						Documentation: m.Doc,
					})
				}
				return reply(ctx, items, nil)
			}
		}
		return reply(ctx, nil, nil)
	default:
		return reply(ctx, nil, nil)
	}
}

func completionPackageIdent(ctx context.Context, s *server, reply jsonrpc2.Replier, params protocol.CompletionParams, pgf *ParsedGnoFile, i *ast.Ident, includeFuncs bool) error {
	for _, spec := range pgf.File.Imports {
		path := spec.Path.Value[1 : len(spec.Path.Value)-1]
		parts := strings.Split(path, "/")
		last := parts[len(parts)-1]
		if last == i.Name {
			pkg := s.completionStore.lookupPkg(last)
			if pkg != nil {
				items := []protocol.CompletionItem{}
				if includeFuncs {
					for _, f := range pkg.Functions {
						if !f.IsExported() {
							continue
						}
						items = append(items, protocol.CompletionItem{
							Label:         f.Name,
							InsertText:    f.Name + "()",
							Kind:          protocol.CompletionItemKindFunction,
							Detail:        f.Signature,
							Documentation: f.Doc,
						})
					}
				}
				for _, s := range pkg.Symbols {
					if s.Kind == "func" {
						continue
					}
					if !unicode.IsUpper(rune(s.Name[0])) {
						continue
					}
					items = append(items, protocol.CompletionItem{
						Label:         s.Name,
						InsertText:    s.Name,
						Kind:          symbolToKind(s.Kind),
						Detail:        s.Signature,
						Documentation: s.Doc,
					})
				}
				return reply(ctx, items, nil)
			}
		}
	}

	return reply(ctx, nil, nil)
}

// End
// ------------------------------------------------------

func InitCompletionStore(dirs []string) *CompletionStore {
	pkgs := []*Package{}

	if len(dirs) == 0 {
		return &CompletionStore{
			pkgs: pkgs,
			time: time.Now(),
		}
	}

	pkgDirs, err := ListGnoPackages(dirs)
	if err != nil {
		// Ignore error
		return &CompletionStore{
			pkgs: pkgs,
			time: time.Now(),
		}
	}

	for _, p := range pkgDirs {
		pkg, err := PackageFromDir(p, false)
		if err != nil {
			continue
		}
		pkgs = append(pkgs, pkg)
	}

	return &CompletionStore{
		pkgs: pkgs,
		time: time.Now(),
	}
}

func PackageFromDir(path string, onlyExports bool) (*Package, error) {
	files, err := ListGnoFiles(path)
	if err != nil {
		return nil, err
	}

	gm, gmErr := gnomod.ParseAt(path)

	var symbols []*Symbol
	var functions []*Function
	var structures []*Structure
	var packageName string
	methods := cmap.New[[]*Method]()
	for _, fname := range files {
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
		// Parse the file and create an AST.
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, fname, text, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if onlyExports { // Trim AST to exported declarations only.
			ast.FileExports(file)
		}

		packageName = file.Name.Name
		ast.Inspect(file, func(n ast.Node) bool {
			var symbol *Symbol

			switch t := n.(type) {
			case *ast.FuncDecl:
				if t.Recv != nil { // method
					if t.Recv.NumFields() > 0 && t.Recv.List[0].Type != nil {
						switch rt := t.Recv.List[0].Type.(type) {
						case *ast.StarExpr:
							k := fmt.Sprintf("%s", rt.X)
							m := &Method{
								Position:  fset.Position(t.Pos()),
								FileURI:   getURI(absPath),
								Name:      t.Name.Name,
								Arguments: []*Field{}, // TODO: fill args
								Doc:       t.Doc.Text(),
								Signature: strings.Split(text[t.Pos()-1:t.End()-1], " {")[0], // TODO: use ast
								Kind:      "func",
							}
							if v, ok := methods.Get(k); ok {
								v = append(v, m)
								methods.Set(k, v)
							} else {
								methods.Set(k, []*Method{m})
							}
						case *ast.Ident:
							k := fmt.Sprintf("%s", rt.Name)
							m := &Method{
								Position:  fset.Position(t.Pos()),
								FileURI:   getURI(absPath),
								Name:      t.Name.Name,
								Arguments: []*Field{}, // TODO: fill args
								Doc:       t.Doc.Text(),
								Signature: strings.Split(text[t.Pos()-1:t.End()-1], " {")[0], // TODO: use ast
								Kind:      "func",
							}
							if v, ok := methods.Get(k); ok {
								v = append(v, m)
								methods.Set(k, v)
							} else {
								methods.Set(k, []*Method{m})
							}
						}
					}
				} else { // func
					f := &Function{
						Position:  fset.Position(t.Pos()),
						FileURI:   getURI(absPath),
						Name:      t.Name.Name,
						Arguments: []*Field{}, // TODO: fill args
						Doc:       t.Doc.Text(),
						Signature: strings.Split(text[t.Pos()-1:t.End()-1], " {")[0], // TODO: use ast
						Kind:      "func",
					}
					functions = append(functions, f)
				}
				symbol = function(n, text)
			case *ast.GenDecl:
				for _, spec := range t.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						switch tt := s.Type.(type) {
						case *ast.StructType:
							buf := new(strings.Builder)
							format.Node(buf, fset, tt)
							structures = append(structures, &Structure{
								Position: fset.Position(tt.Pos()),
								FileURI:  getURI(absPath),
								Name:     s.Name.Name,
								Fields:   []*Field{}, // TODO: fill fields
								Doc:      t.Doc.Text(),
								String:   buf.String(),
							})
						}
					}
				}
				symbol = declaration(n, text)
			}

			if symbol != nil {
				symbol.FileURI = getURI(absPath)
				symbol.Position = fset.Position(n.Pos())
				symbols = append(symbols, symbol)
			}

			return true
		})
	}
	return &Package{
		Name: packageName,
		ImportPath: func() string {
			if gmErr != nil {
				return packageName
			}
			return gm.Module.Mod.Path
		}(),
		Symbols:    symbols,
		Functions:  functions,
		Methods:    methods,
		Structures: structures,
	}, nil
}

func getSymbols(fname string) []*Symbol {
	var symbols []*Symbol
	absPath, err := filepath.Abs(fname)
	if err != nil {
		return symbols // Ignore error and return empty symbol list
	}
	bsrc, err := os.ReadFile(absPath)
	if err != nil {
		return symbols // Ignore error and return empty symbol list
	}
	text := string(bsrc)

	// Parse the file and create an AST.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fname, nil, parser.ParseComments)
	if err != nil {
		// Ignore error and return empty symbol list
		return symbols
	}

	// Trim AST to exported declarations only.
	ast.FileExports(file)

	ast.Inspect(file, func(n ast.Node) bool {
		var symbol *Symbol

		switch n.(type) {
		case *ast.FuncDecl:
			symbol = function(n, text)
		case *ast.GenDecl:
			symbol = declaration(n, text)
		}

		if symbol != nil {
			symbol.FileURI = getURI(absPath)
			symbol.Position = fset.Position(n.Pos())
			symbols = append(symbols, symbol)
		}

		return true
	})

	return symbols
}

func declaration(n ast.Node, source string) *Symbol {
	sym, _ := n.(*ast.GenDecl)

	for _, spec := range sym.Specs {
		switch t := spec.(type) {
		case *ast.TypeSpec:
			return &Symbol{
				Name:      t.Name.Name,
				Doc:       sym.Doc.Text(),
				Signature: strings.Split(source[t.Pos()-1:t.End()-1], " {")[0],
				Kind:      typeName(*t),
			}
		}
	}

	return nil
}

func function(n ast.Node, source string) *Symbol {
	sym, _ := n.(*ast.FuncDecl)
	return &Symbol{
		Name:      sym.Name.Name,
		Doc:       sym.Doc.Text(),
		Signature: strings.Split(source[sym.Pos()-1:sym.End()-1], " {")[0],
		Kind:      "func",
	}
}

func typeName(t ast.TypeSpec) string {
	switch t.Type.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	case *ast.ArrayType:
		return "array"
	case *ast.MapType:
		return "map"
	case *ast.ChanType:
		return "chan"
	default:
		return "type"
	}
}

func getURI(absFilePath string) uri.URI {
	return uri.URI("file://" + absFilePath)
}
