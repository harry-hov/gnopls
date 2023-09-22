package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
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
}

type Symbol struct {
	Name      string
	Doc       string
	Signature string
	Kind      string
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
				Label:         s.Name,
				InsertText:    s.Name,
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
		files, err := ListGnoFiles(p)
		if err != nil {
			// Ignore error
			// Continue with rest of the packages
			continue
		}
		symbols := []*Symbol{}
		for _, file := range files {
			symbols = append(symbols, getSymbols(file)...)
		}
		// convert to import path:
		// get path relative to dir, and convert separators to slashes.
		ip := strings.ReplaceAll(
			strings.TrimPrefix(p, p+string(filepath.Separator)),
			string(filepath.Separator), "/",
		)

		pkgs = append(pkgs, &Package{
			Name:       filepath.Base(p),
			ImportPath: ip,
			Symbols:    symbols,
		})
	}

	return &CompletionStore{
		pkgs: pkgs,
		time: time.Now(),
	}
}

func getSymbols(fname string) []*Symbol {
	var symbols []*Symbol

	bsrc, err := os.ReadFile(fname)
	if err != nil {
		// Ignore error and return empty symbol list
		return symbols
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
		var found *Symbol

		switch n.(type) {
		case *ast.FuncDecl:
			found = function(n, text)
		case *ast.GenDecl:
			found = declaration(n, text)
		}

		if found != nil {
			symbols = append(symbols, found)
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
