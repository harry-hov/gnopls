package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"path/filepath"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func (s *server) Definition(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DefinitionParams
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
	pgf, err := file.ParseGno(ctx)
	if err != nil {
		return reply(ctx, nil, errors.New("cannot parse gno file"))
	}
	// Load pkg from cache
	pkg, ok := s.cache.pkgs.Get(filepath.Dir(string(params.TextDocument.URI.Filename())))
	if !ok {
		return reply(ctx, nil, nil)
	}
	info := pkg.TypeCheckResult.info

	// Calculate offset and line
	offset := file.PositionToOffset(params.Position)
	line := params.Position.Line + 1 // starts at 0, so adding 1

	slog.Info("definition", "offset", offset)

	// Handle definition for import paths
	for _, spec := range pgf.File.Imports {
		// Inclusive of the end points
		if spec.Path.Pos() <= token.Pos(offset) && token.Pos(offset) <= spec.Path.End() {
			path := spec.Path.Value[1 : len(spec.Path.Value)-1]
			parts := strings.Split(path, "/")
			last := parts[len(parts)-1]
			pkg := s.completionStore.lookupPkg(last)
			if pkg == nil {
				slog.Info("")
				return reply(ctx, nil, nil)
			}
			if len(pkg.Symbols) < 1 {
				return reply(ctx, nil, nil)
			}
			return reply(ctx, protocol.Location{
				URI: pkg.Symbols[0].FileURI,
				Range: *posToRange(
					1,
					[]int{0, 0},
				),
			}, nil)
		}
	}

	// Get path enclosing
	paths := pathEnclosingObjNode(pgf.File, token.Pos(offset))
	if len(paths) < 2 {
		return reply(ctx, nil, nil)
	}

	switch n := paths[0].(type) {
	case *ast.Ident:
		_, tv := getTypeAndValue(
			*pkg.TypeCheckResult.fset,
			info, n.Name,
			int(line),
			offset,
		)
		if tv == nil || tv.Type == nil {
			switch t := paths[1].(type) {
			case *ast.FuncDecl:
				if t.Recv != nil {
					return definitionMethodDecl(ctx, reply, params, pkg, n, t)
				}
				return definitionFuncDecl(ctx, reply, params, pkg, n)
			case *ast.SelectorExpr:
				return definitionSelectorExpr(ctx, s, reply, params, pgf, pkg, paths, n, t, int(line))
			default:
				return reply(ctx, nil, nil)
			}
		}
		typeStr := tv.Type.String()
		m := mode(*tv)
		isPackageLevelGlobal := strings.Contains(typeStr, pkg.ImportPath) // better name

		// Handle builtins
		if _, ok := isBuiltin(n, tv); ok {
			return reply(ctx, nil, nil)
		}

		// local var
		if (isPackageLevelGlobal || !strings.Contains(typeStr, "gno.land")) && m == "var" {
			return reply(ctx, nil, nil)
		}

		// local type
		if isPackageLevelGlobal && m == "type" {
			typeStr := parseType(typeStr, pkg.ImportPath)
			return definitionPackageLevelTypes(ctx, reply, params, pkg, n, tv, m, typeStr)
		}

		// local global and is value
		if m == "value" {
			typeStr := parseType(typeStr, pkg.ImportPath)
			return definitionPackageLevelValue(ctx, reply, params, pkg, n, tv, m, typeStr, isPackageLevelGlobal)
		}

		return reply(ctx, nil, nil)
	default:
		return reply(ctx, nil, nil)
	}
}

func definitionSelectorExpr(ctx context.Context, s *server, reply jsonrpc2.Replier, params protocol.DefinitionParams, pgf *ParsedGnoFile, pkg *Package, paths []ast.Node, i *ast.Ident, sel *ast.SelectorExpr, line int) error {
	exprStr := types.ExprString(sel)

	parent := sel.X
	parentStr := types.ExprString(parent)

	_, tv := getTypeAndValueLight(
		*pkg.TypeCheckResult.fset,
		pkg.TypeCheckResult.info,
		exprStr,
		int(line),
	)
	if tv == nil || tv.Type == nil {
		return reply(ctx, nil, nil)
	}
	tvStr := tv.Type.String()

	_, tvParent := getTypeAndValueLight(
		*pkg.TypeCheckResult.fset,
		pkg.TypeCheckResult.info,
		parentStr,
		int(line),
	)
	if tvParent == nil { // can be import
		for _, spec := range pgf.File.Imports {
			path := spec.Path.Value[1 : len(spec.Path.Value)-1]
			parts := strings.Split(path, "/")
			last := parts[len(parts)-1]
			if last == i.Name { // on pkg name
				return reply(ctx, protocol.Location{
					URI: params.TextDocument.URI,
					Range: *posToRange(
						int(1),
						[]int{0, 0},
					),
				}, nil)
			} else if last == parentStr { // on package symbol
				symbol := s.completionStore.lookupSymbol(parentStr, i.Name)
				if symbol == nil {
					break
				}

				fileUri := symbol.FileURI
				pos := symbol.Position

				return reply(ctx, protocol.Location{
					URI: fileUri,
					Range: *posToRange(
						int(pos.Line),
						[]int{pos.Offset, pos.Offset},
					),
				}, nil)
			}
		}
		return reply(ctx, nil, nil)
	}
	tvParentStr := tvParent.Type.String()

	if strings.Contains(tvStr, "func") {
		if strings.Contains(tvParentStr, pkg.ImportPath) {
			return definitionFuncDecl(ctx, reply, params, pkg, i)
		}

		for _, spec := range pgf.File.Imports {
			path := spec.Path.Value[1 : len(spec.Path.Value)-1]
			if strings.Contains(tvParentStr, path) { // hover on parent var of kind import
				parts := strings.Split(path, "/")
				last := parts[len(parts)-1]
				pkg := s.completionStore.lookupPkg(last)
				if pkg == nil {
					break
				}
				tvParentStrParts := strings.Split(tvParentStr, ".")
				parentType := tvParentStrParts[len(tvParentStrParts)-1]
				methods, ok := pkg.Methods.Get(parentType)
				if !ok {
					break
				}
				var fileUri uri.URI
				var pos token.Position
				for _, m := range methods {
					if m.Name == i.Name {
						fileUri = m.FileURI
						pos = m.Position
					}
				}

				if fileUri == "" {
					break
				}

				return reply(ctx, protocol.Location{
					URI: fileUri,
					Range: *posToRange(
						int(pos.Line),
						[]int{pos.Offset, pos.Offset},
					),
				}, nil)
			}

		}

		// can be non gno.land import
		// TODO: check if method has the same reciever
		// TODO: better load the package and check Methods map
		for _, spec := range pgf.File.Imports {
			if strings.Contains(spec.Path.Value, "gno.land") {
				continue
			}
			path := spec.Path.Value[1 : len(spec.Path.Value)-1]
			symbol := s.completionStore.lookupSymbol(path, i.Name)
			if symbol == nil {
				continue
			}
			if symbol.Kind != "func" {
				continue
			}

			fileUri := symbol.FileURI
			pos := symbol.Position

			return reply(ctx, protocol.Location{
				URI: fileUri,
				Range: *posToRange(
					int(pos.Line),
					[]int{pos.Offset, pos.Offset},
				),
			}, nil)
		}
	}

	return reply(ctx, nil, nil)
}

func definitionMethodDecl(ctx context.Context, reply jsonrpc2.Replier, params protocol.DefinitionParams, pkg *Package, i *ast.Ident, decl *ast.FuncDecl) error {
	if decl.Recv.NumFields() != 1 || decl.Recv.List[0].Type == nil {
		return reply(ctx, nil, nil)
	}

	var key string
	switch rt := decl.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		key = fmt.Sprintf("%s", rt.X)
	case *ast.Ident:
		key = fmt.Sprintf("%s", rt.Name)
	default:
		return reply(ctx, nil, nil)
	}

	methods, ok := pkg.Methods.Get(key)
	if !ok {
		return reply(ctx, nil, nil)
	}

	var fileUri uri.URI
	var pos token.Position
	for _, m := range methods {
		if m.Name == i.Name {
			fileUri = m.FileURI
			pos = m.Position
			break
		}
	}

	if fileUri == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Location{
		URI: fileUri,
		Range: *posToRange(
			int(pos.Line),
			[]int{pos.Offset, pos.Offset},
		),
	}, nil)
}

// TODO: handle var doc
func definitionFuncDecl(ctx context.Context, reply jsonrpc2.Replier, params protocol.DefinitionParams, pkg *Package, i *ast.Ident) error {
	var fileUri uri.URI
	var pos token.Position
	for _, s := range pkg.Symbols {
		if s.Name == i.Name {
			fileUri = s.FileURI
			pos = s.Position
			break
		}
	}

	if fileUri == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Location{
		URI: fileUri,
		Range: *posToRange(
			int(pos.Line),
			[]int{pos.Offset, pos.Offset},
		),
	}, nil)
}

func definitionPackageLevelValue(ctx context.Context, reply jsonrpc2.Replier, params protocol.DefinitionParams, pkg *Package, i *ast.Ident, tv *types.TypeAndValue, mode, typeStr string, isPackageLevelGlobal bool) error {
	var fileUri uri.URI
	var pos token.Position
	for _, s := range pkg.Symbols {
		if s.Name == i.Name {
			fileUri = s.FileURI
			pos = s.Position
			break
		}
	}

	if fileUri == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Location{
		URI: fileUri,
		Range: *posToRange(
			int(pos.Line),
			[]int{pos.Offset, pos.Offset},
		),
	}, nil)
}

func definitionPackageLevelTypes(ctx context.Context, reply jsonrpc2.Replier, params protocol.DefinitionParams, pkg *Package, i *ast.Ident, tv *types.TypeAndValue, mode, typeName string) error {
	// Look into structures
	var structure *Structure
	for _, st := range pkg.Structures {
		if st.Name == fmt.Sprintf("%s", typeName) {
			structure = st
			break
		}
	}
	var fileUri uri.URI
	var pos token.Position
	if structure != nil {
		fileUri = structure.FileURI
		pos = structure.Position
	} else { // If not in structures, look into symbols
		for _, s := range pkg.Symbols {
			if s.Name == i.Name {
				fileUri = s.FileURI
				pos = s.Position
				break
			}
		}
	}

	if fileUri == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Location{
		URI: fileUri,
		Range: *posToRange(
			int(pos.Line),
			[]int{pos.Offset, pos.Offset},
		),
	}, nil)
}
