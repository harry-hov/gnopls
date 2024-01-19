package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
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
	var params protocol.DefinitionParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	file, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}

	offset := file.PositionToOffset(params.Position)
	line := params.Position.Line
	// tokedf := pgf.FileSet.AddFile(doc.Path, -1, len(doc.Content))
	// target := tokedf.Pos(offset)

	slog.Info("hover", "line", line, "offset", offset)
	pgf, err := file.ParseGno(ctx)
	if err != nil {
		return reply(ctx, nil, errors.New("cannot parse gno file"))
	}

	var expr ast.Expr
	ast.Inspect(pgf.File, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && pgf.Fset.Position(e.Pos()).Line == int(line+1) {
			expr = e
			return false
		}
		return true
	})

	// TODO: Remove duplicate code
	switch e := expr.(type) {
	case *ast.CallExpr:
		slog.Info("hover - CALL_EXPR")
		switch v := e.Fun.(type) {
		case *ast.Ident:
			// TODO: don't show methods
			pkgPath := filepath.Dir(params.TextDocument.URI.Filename())
			sym, ok := s.cache.lookupSymbol(pkgPath, v.Name)
			if !ok {
				return reply(ctx, nil, nil)
			}
			return reply(ctx, protocol.Hover{
				Contents: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: sym.String(),
				},
				Range: posToRange(
					int(params.Position.Line),
					[]int{0, 4},
				),
			}, nil)
		case *ast.SelectorExpr:
			// case pkg.Func
			i, ok := v.X.(*ast.Ident)
			if !ok {
				return reply(ctx, nil, nil)
			}

			if offset >= int(i.Pos())-1 && offset < int(i.End())-1 { // pkg or var
				if i.Obj != nil { // var
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
					return reply(ctx, nil, nil)
				}

				for _, spec := range pgf.File.Imports { // pkg
					// remove leading and trailing `"`
					path := spec.Path.Value[1 : len(spec.Path.Value)-1]
					parts := strings.Split(path, "/")
					last := parts[len(parts)-1]
					if last == i.Name {
						header := fmt.Sprintf("```gno\npackage %s (%s)\n```\n\n", last, spec.Path.Value)
						body := func() string {
							if strings.HasPrefix(path, "gno.land/") {
								return fmt.Sprintf("[```%s``` on gno.land](https://%s)", last, path)
							}
							return fmt.Sprintf("[```%s``` on gno.land](https://gno.land)", last)
						}()
						return reply(ctx, protocol.Hover{
							Contents: protocol.MarkupContent{
								Kind:  protocol.Markdown,
								Value: header + body,
							},
							Range: posToRange(
								int(params.Position.Line),
								[]int{int(i.Pos()), int(i.End())},
							),
						}, nil)
					}
				}
			} else if offset >= int(e.Pos())-1 && offset < int(e.End())-1 { // Func
				symbol := s.completionStore.lookupSymbol(i.Name, v.Sel.Name)
				if symbol != nil {
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: symbol.String(),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(e.Pos()), int(e.End())},
						),
					}, nil)
				}
			}
		default:
			return reply(ctx, nil, nil)
		}
		return reply(ctx, nil, nil)
	case *ast.SelectorExpr:
		slog.Info("hover - SELECTOR_EXPR")
		// we have a format X.A
		i, ok := e.X.(*ast.Ident)
		if !ok {
			return reply(ctx, nil, nil)
		}
		if i.Obj != nil { // its a var
			if offset >= int(i.Pos())-1 && offset < int(i.End())-1 {
				return reply(ctx, protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.Markdown,
						Value: fmt.Sprintf("```gno\n%s %s\n```", i.Obj.Kind, i.Obj.Name),
					},
					Range: posToRange(
						int(params.Position.Line),
						[]int{int(i.Pos()), int(i.End())},
					),
				}, nil)
			}
			return reply(ctx, nil, nil)
		}
		if offset >= int(i.Pos())-1 && offset < int(i.End())-1 { // X
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
		} else if offset >= int(e.Pos())-1 && offset < int(e.End())-1 { // A
			symbol := s.completionStore.lookupSymbol(i.Name, e.Sel.Name)
			if symbol != nil {
				return reply(ctx, protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.Markdown,
						Value: symbol.String(),
					},
					Range: posToRange(
						int(params.Position.Line),
						[]int{int(e.Pos()), int(e.End())},
					),
				}, nil)
			}
		}
		// slog.Info("SELECTOR_EXPR", "name", e.Sel.Name, "obj", e.Sel.String())
	case *ast.FuncType:
		var funcDecl *ast.FuncDecl
		ast.Inspect(pgf.File, func(n ast.Node) bool {
			if f, ok := n.(*ast.FuncDecl); ok && f.Type == e {
				funcDecl = f
				return false
			}
			return true
		})
		if funcDecl == nil {
			return reply(ctx, nil, nil)
		}
		if funcDecl.Recv != nil {
			// slog.Info("FUNC-TYPE", "pos", funcDecl.Recv.List[0].Type.Pos(), "end", funcDecl.Recv.List[0].Type.End())
			if offset >= int(funcDecl.Recv.List[0].Type.Pos())-1 && offset < int(funcDecl.Recv.List[0].Type.End())-1 {
				switch t := funcDecl.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					k := fmt.Sprintf("*%s", t.X)
					pkg, ok := s.cache.pkgs.Get(filepath.Dir(string(params.TextDocument.URI.Filename())))
					if !ok {
						return reply(ctx, nil, nil)
					}
					var structure *Structure
					for _, st := range pkg.Structures {
						if st.Name == fmt.Sprintf("%s", t.X) {
							structure = st
							break
						}
					}
					if structure == nil {
						return reply(ctx, nil, nil)
					}
					var header, body string
					header = fmt.Sprintf("type %s %s\n\n", structure.Name, structure.String)
					methods, ok := pkg.Methods.Get(k)
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
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, body),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(t.Pos()), int(t.End())},
						),
					}, nil)
				case *ast.Ident:
					header := fmt.Sprintf("var %s", t.Name)
					return reply(ctx, protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.Markdown,
							Value: FormatHoverContent(header, ""),
						},
						Range: posToRange(
							int(params.Position.Line),
							[]int{int(t.Pos()), int(t.End())},
						),
					}, nil)
				}
			}
		}
	default:
		slog.Info("hover - NOT HANDLED")
	}

	return reply(ctx, nil, nil)
}

func FormatHoverContent(header, body string) string {
	return fmt.Sprintf("```gno\n%s\n```\n\n%s", header, body)
}

// handleSelectorExpr returns jsonrpc2.Replier for Hover
// on SelectorExpr
// TODO: Move duplicate logic here
// func handleSelectorExpr(expr *ast.SelectorExpr) jsonrpc2.Replier {
// }
