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
)

type HoveredToken struct {
	Text  string
	Start int
	End   int
}

func (s *server) Hover(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.HoverParams
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

	slog.Info("hover", "line", line, "offset", offset)

	// Handle hovering over import paths
	for _, spec := range pgf.File.Imports {
		// Inclusive of the end points
		if spec.Path.Pos() <= token.Pos(offset) && token.Pos(offset) <= spec.Path.End() {
			return hoverImport(ctx, reply, pgf, params, spec)
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
					return hoverMethodDecl(ctx, reply, params, pkg, n, t)
				}
				return hoverFuncDecl(ctx, reply, params, pkg, n)
			case *ast.SelectorExpr:
				return hoverSelectorExpr(ctx, s, reply, params, pgf, pkg, paths, n, t, int(line))
			default:
				return reply(ctx, protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.Markdown,
						Value: FormatHoverContent(n.Name, ""),
					},
					Range: posToRange(
						int(params.Position.Line),
						[]int{int(n.Pos()), int(n.End())},
					),
				}, nil)
			}
		}
		typeStr := tv.Type.String()
		m := mode(*tv)
		isPackageLevelGlobal := strings.Contains(typeStr, pkg.ImportPath) // better name

		// Handle builtins
		if doc, ok := isBuiltin(n, tv); ok {
			return hoverBuiltinTypes(ctx, reply, params, n, tv, m, doc)
		}

		// local var
		if (isPackageLevelGlobal || !strings.Contains(typeStr, "gno.land")) && m == "var" {
			return hoverLocalVar(ctx, reply, params, pkg, n, tv, m, typeStr, isPackageLevelGlobal)
		}

		// local type
		if isPackageLevelGlobal && m == "type" {
			typeStr := parseType(typeStr, pkg.ImportPath)
			return hoverPackageLevelTypes(ctx, reply, params, pkg, n, tv, m, typeStr)
		}

		// local global and is value
		if m == "value" {
			typeStr := parseType(typeStr, pkg.ImportPath)
			return hoverPackageLevelValue(ctx, reply, params, pkg, n, tv, m, typeStr, isPackageLevelGlobal)
		}

		// if var of type imported package
		var header string
		if strings.Contains(typeStr, "gno.land/") {
			for _, spec := range pgf.File.Imports {
				path := spec.Path.Value[1 : len(spec.Path.Value)-1]
				if strings.Contains(typeStr, path) {
					parts := strings.Split(path, "/")
					last := parts[len(parts)-1]
					t := strings.Replace(typeStr, path, last, 1)
					header = fmt.Sprintf("%s %s %s", m, n.Name, t)
					break
				}
			}
		} else { // rest of the cases
			header = fmt.Sprintf("%s %s %s", m, n.Name, typeStr)
		}

		// Handles rest of the cases
		// TODO: improve?
		return reply(ctx, protocol.Hover{
			Contents: protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: FormatHoverContent(header, ""),
			},
			Range: posToRange(
				int(params.Position.Line),
				[]int{int(n.Pos()), int(n.End())},
			),
		}, nil)
	default:
		return reply(ctx, nil, nil)
	}
}

func hoverSelectorExpr(ctx context.Context, s *server, reply jsonrpc2.Replier, params protocol.HoverParams, pgf *ParsedGnoFile, pkg *Package, paths []ast.Node, i *ast.Ident, sel *ast.SelectorExpr, line int) error {
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
			if last == i.Name { // hover on pkg name
				header := fmt.Sprintf("package %s (%s)", last, spec.Path.Value)
				body := func() string {
					if strings.HasPrefix(path, "gno.land/") {
						return fmt.Sprintf("[```%s``` on gno.land](https://%s)", last, path)
					}
					return fmt.Sprintf("[```%s``` on gno.land](https://gno.land)", last)
				}()
				return reply(ctx, protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.Markdown,
						Value: FormatHoverContent(header, body),
					},
					Range: posToRange(
						int(params.Position.Line),
						[]int{int(i.Pos()), int(i.End())},
					),
				}, nil)
			} else if last == parentStr { // hover on package symbol
				symbol := s.completionStore.lookupSymbol(parentStr, i.Name)
				if symbol == nil {
					break
				}

				return reply(ctx, protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.Markdown,
						Value: symbol.String(),
					},
					Range: posToRange(
						int(params.Position.Line),
						[]int{int(i.Pos()), int(i.End())},
					),
				}, nil)
			}
		}
		return reply(ctx, nil, nil)
	}
	tvParentStr := tvParent.Type.String()

	if strings.Contains(tvStr, "func") {
		if strings.Contains(tvParentStr, pkg.ImportPath) {
			return hoverFuncDecl(ctx, reply, params, pkg, i)
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
				var header, body string
				for _, m := range methods {
					if m.Name == i.Name {
						header = m.Signature
						body = m.Doc
					}
				}
				if header == "" {
					break
				}

				return reply(ctx, protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.Markdown,
						Value: FormatHoverContent(header, body),
					},
					Range: posToRange(
						int(params.Position.Line),
						[]int{int(i.Pos()), int(i.End())},
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

			return reply(ctx, protocol.Hover{
				Contents: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: symbol.String(),
				},
				Range: posToRange(
					int(params.Position.Line),
					[]int{int(i.Pos()), int(i.End())},
				),
			}, nil)
		}
	} else {
		t := tvStr
		if strings.Contains(tvStr, pkg.ImportPath) {
			t = strings.Replace(tvStr, pkg.ImportPath+".", "", 1)
		}
		header := fmt.Sprintf("%s %s %s", mode(*tv), i.Name, t)
		return reply(ctx, protocol.Hover{
			Contents: protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: FormatHoverContent(header, ""),
			},
			Range: posToRange(
				int(params.Position.Line),
				[]int{int(i.Pos()), int(i.End())},
			),
		}, nil)
	}

	return reply(ctx, nil, nil)
}

func hoverMethodDecl(ctx context.Context, reply jsonrpc2.Replier, params protocol.HoverParams, pkg *Package, i *ast.Ident, decl *ast.FuncDecl) error {
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

	var header, body string
	for _, m := range methods {
		if m.Name == i.Name {
			header = m.Signature
			body = m.Doc
			break
		}
	}

	if header == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, body),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(i.Pos()), int(i.End())},
		),
	}, nil)
}

// TODO: handle var doc
func hoverFuncDecl(ctx context.Context, reply jsonrpc2.Replier, params protocol.HoverParams, pkg *Package, i *ast.Ident) error {
	var header, body string
	for _, s := range pkg.Symbols {
		if s.Name == i.Name {
			header = s.Signature
			body = s.Doc
			break
		}
	}

	if header == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, body),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(i.Pos()), int(i.End())},
		),
	}, nil)
}

// TODO: handle var doc
func hoverPackageLevelValue(ctx context.Context, reply jsonrpc2.Replier, params protocol.HoverParams, pkg *Package, i *ast.Ident, tv *types.TypeAndValue, mode, typeStr string, isPackageLevelGlobal bool) error {
	var header, body string
	for _, s := range pkg.Symbols {
		if s.Name == i.Name {
			header = s.Signature
			body = s.Doc
			break
		}
	}

	if header == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, body),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(i.Pos()), int(i.End())},
		),
	}, nil)
}

// TODO: handle var doc
func hoverLocalVar(ctx context.Context, reply jsonrpc2.Replier, params protocol.HoverParams, pkg *Package, i *ast.Ident, tv *types.TypeAndValue, mode, typeStr string, isLocalGlobal bool) error {
	t := typeStr
	if isLocalGlobal {
		t = strings.Replace(typeStr, pkg.ImportPath+".", "", 1)
	}

	header := fmt.Sprintf("%s %s %s", mode, i.Name, t)
	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, ""),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(i.Pos()), int(i.End())},
		),
	}, nil)
}

func hoverPackageLevelTypes(ctx context.Context, reply jsonrpc2.Replier, params protocol.HoverParams, pkg *Package, i *ast.Ident, tv *types.TypeAndValue, mode, typeName string) error {
	// Look into structures
	var structure *Structure
	for _, st := range pkg.Structures {
		if st.Name == fmt.Sprintf("%s", typeName) {
			structure = st
			break
		}
	}
	var header, body string
	if structure != nil {
		header = fmt.Sprintf("%s %s %s\n\n", mode, structure.Name, structure.String)
		methods, ok := pkg.Methods.Get(typeName)
		if ok {
			body = "```gno\n"
			for _, m := range methods {
				if m.IsExported() {
					body += fmt.Sprintf("%s\n", m.Signature)
				}
			}
			body += "```\n"
			body += structure.Doc + "\n"
		}
	} else { // If not in structures, look into symbols
		for _, s := range pkg.Symbols {
			if s.Name == i.Name {
				header = fmt.Sprintf("%s %s", mode, s.Signature)
				body = s.Doc
				break
			}
		}
	}

	if header == "" {
		return reply(ctx, nil, nil)
	}

	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, body),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(i.Pos()), int(i.End())},
		),
	}, nil)
}

func hoverBuiltinTypes(ctx context.Context, reply jsonrpc2.Replier, params protocol.HoverParams, i *ast.Ident, tv *types.TypeAndValue, mode, doc string) error {
	t := tv.Type.String()
	var header string
	if t == "nil" || t == "untyped nil" { // special case?
		header = "var nil Type"
	} else if strings.HasPrefix(t, "func") && mode == "builtin" {
		header = i.Name + strings.TrimPrefix(t, "func")
	} else if (i.Name == "true" || i.Name == "false") && t == "bool" {
		header = `const (
	true	= 0 == 0	// Untyped bool.
	false	= 0 != 0	// Untyped bool.
)`
	} else {
		header = fmt.Sprintf("%s %s %s", mode, i.Name, t)
	}

	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, doc),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(i.Pos()), int(i.End())},
		),
	}, nil)
}

// TODO: check if imports exists in `examples` or `stdlibs`
func hoverImport(ctx context.Context, reply jsonrpc2.Replier, pgf *ParsedGnoFile, params protocol.HoverParams, spec *ast.ImportSpec) error {
	// remove leading and trailing `"`
	path := spec.Path.Value[1 : len(spec.Path.Value)-1]
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]

	header := fmt.Sprintf("package %s (%s)", last, spec.Path.Value)
	body := func() string {
		if strings.HasPrefix(path, "gno.land/") {
			return fmt.Sprintf("[```%s``` on gno.land](https://%s)", last, path)
		}
		return fmt.Sprintf("[```%s``` on gno.land](https://gno.land)", last)
	}()
	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: FormatHoverContent(header, body),
		},
		Range: posToRange(
			int(params.Position.Line),
			[]int{int(spec.Pos()), int(spec.End())},
		),
	}, nil)
}

func hoverPackageIdent(ctx context.Context, reply jsonrpc2.Replier, pgf *ParsedGnoFile, params protocol.HoverParams, i *ast.Ident) error {
	for _, spec := range pgf.File.Imports {
		// remove leading and trailing `"`
		path := spec.Path.Value[1 : len(spec.Path.Value)-1]
		parts := strings.Split(path, "/")
		last := parts[len(parts)-1]
		if last == i.Name {
			header := fmt.Sprintf("package %s (%s)", last, spec.Path.Value)
			body := func() string {
				if strings.HasPrefix(path, "gno.land/") {
					return fmt.Sprintf("[```%s``` on gno.land](https://%s)", last, path)
				}
				return fmt.Sprintf("[```%s``` on gno.land](https://gno.land)", last)
			}()
			return reply(ctx, protocol.Hover{
				Contents: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: FormatHoverContent(header, body),
				},
				Range: posToRange(
					int(params.Position.Line),
					[]int{int(i.Pos()), int(i.End())},
				),
			}, nil)
		}
	}
	return reply(ctx, nil, nil)
}

func hoverVariableIdent(ctx context.Context, reply jsonrpc2.Replier, pgf *ParsedGnoFile, params protocol.HoverParams, i *ast.Ident) error {
	if i.Obj != nil {
		switch u := i.Obj.Decl.(type) {
		case *ast.Field:
			if u.Type != nil {
				switch t := u.Type.(type) {
				case *ast.StarExpr:
					header := fmt.Sprintf("%s %s *%s", i.Obj.Kind, u.Names[0], t.X)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(i.Pos()), int(i.End())},
						),
					}, nil)
				case *ast.Ident:
					header := fmt.Sprintf("%s %s %s", i.Obj.Kind, u.Names[0], t.Name)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(i.Pos()), int(i.End())},
						),
					}, nil)
				}
			}
		case *ast.TypeSpec:
			if u.Type != nil {
				switch t := u.Type.(type) {
				case *ast.StarExpr:
					header := fmt.Sprintf("%s %s *%s", i.Obj.Kind, u.Name, t.X)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(i.Pos()), int(i.End())},
						),
					}, nil)
				case *ast.Ident:
					header := fmt.Sprintf("%s %s %s", i.Obj.Kind, u.Name, t.Name)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(i.Pos()), int(i.End())},
						),
					}, nil)
				}
			}
		case *ast.ValueSpec:
			if u.Type != nil {
				switch t := u.Type.(type) {
				case *ast.StarExpr:
					header := fmt.Sprintf("%s %s *%s", i.Obj.Kind, u.Names[0], t.X)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(i.Pos()), int(i.End())},
						),
					}, nil)
				case *ast.Ident:
					header := fmt.Sprintf("%s %s %s", i.Obj.Kind, u.Names[0], t.Name)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(i.Pos()), int(i.End())},
						),
					}, nil)
				}
			}
		default:
			slog.Info("hover", "NOT HANDLED", u)
		}
	}
	return reply(ctx, nil, nil)
}

func FormatHoverContent(header, body string) string {
	return fmt.Sprintf("```gno\n%s\n```\n\n%s", header, body)
}

// getIdentNodes return idents from Expr
// Note: only handles *ast.SelectorExpr and  *ast.CallExpr
func getIdentNodes(n ast.Node) []*ast.Ident {
	res := []*ast.Ident{}
	switch t := n.(type) {
	case *ast.Ident:
		res = append(res, t)
	case *ast.SelectorExpr:
		res = append(res, t.Sel)
		res = append(res, getIdentNodes(t.X)...)
	case *ast.CallExpr:
		res = append(res, getIdentNodes(t.Fun)...)
	}

	return res
}

func getExprAtLine(pgf *ParsedGnoFile, line int) ast.Expr {
	var expr ast.Expr
	ast.Inspect(pgf.File, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && pgf.Fset.Position(e.Pos()).Line == int(line) {
			expr = e
			return false
		}
		return true
	})
	return expr
}

// pathEnclosingObjNode returns the AST path to the object-defining
// node associated with pos. "Object-defining" means either an
// *ast.Ident mapped directly to a types.Object or an ast.Node mapped
// implicitly to a types.Object.
func pathEnclosingObjNode(f *ast.File, pos token.Pos) []ast.Node {
	var (
		path  []ast.Node
		found bool
	)

	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}

		if n == nil {
			path = path[:len(path)-1]
			return false
		}

		path = append(path, n)

		switch n := n.(type) {
		case *ast.Ident:
			// Include the position directly after identifier. This handles
			// the common case where the cursor is right after the
			// identifier the user is currently typing. Previously we
			// handled this by calling astutil.PathEnclosingInterval twice,
			// once for "pos" and once for "pos-1".
			found = n.Pos() <= pos && pos <= n.End()
		case *ast.ImportSpec:
			if n.Path.Pos() <= pos && pos < n.Path.End() {
				found = true
				// If import spec has a name, add name to path even though
				// position isn't in the name.
				if n.Name != nil {
					path = append(path, n.Name)
				}
			}
		case *ast.StarExpr:
			// Follow star expressions to the inner identifier.
			if pos == n.Star {
				pos = n.X.Pos()
			}
		}

		return !found
	})

	if len(path) == 0 {
		return nil
	}

	// Reverse path so leaf is first element.
	for i := 0; i < len(path)/2; i++ {
		path[i], path[len(path)-1-i] = path[len(path)-1-i], path[i]
	}

	return path
}

// parseType parses the type name from full path and return
// the type as string and if it is isStar expr.
func parseType(t, importpath string) string {
	return strings.TrimPrefix(strings.TrimPrefix(t, "*"), importpath+".")
}
