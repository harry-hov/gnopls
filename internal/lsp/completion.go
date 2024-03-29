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
	Kind      string
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

func (s *server) Completion(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.CompletionParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	file, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}

	items := []protocol.CompletionItem{}

	token, err := file.TokenAt(params.Position)
	if err != nil {
		return reply(ctx, nil, err)
	}
	text := strings.TrimSuffix(strings.TrimSpace(token.Text), ".")
	slog.Info("completion", "text", text)

	// TODO:
	// pgf, err := file.ParseGno(ctx)
	// path, e := astutil.PathEnclosingInterval(pgf.File, 13, 8)

	pkg := s.completionStore.lookupPkg(text)
	if pkg != nil {
		for _, s := range pkg.Symbols {
			items = append(items, protocol.CompletionItem{
				Label: s.Name,
				InsertText: func() string {
					if s.Kind == "func" {
						return s.Name + "()"
					}
					return s.Name
				}(),
				Kind:          symbolToKind(s.Kind),
				Detail:        s.Signature,
				Documentation: s.Doc,
			})
		}
	}

	return reply(ctx, items, err)
}

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
