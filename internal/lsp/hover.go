package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"log/slog"
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
			// TODO: is a func? handle.
		case *ast.SelectorExpr:
			// case pkg.Func
			i, ok := v.X.(*ast.Ident)
			if !ok {
				return reply(ctx, nil, nil)
			}
			if offset >= int(i.Pos())-1 && offset < int(i.End())-1 { // pkg
				for _, spec := range pgf.File.Imports {
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
		if offset >= int(i.Pos())-1 && offset < int(i.End())-1 { // X
			for _, spec := range pgf.File.Imports {
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
	default:
		slog.Info("hover - NOT HANDLED")
		return reply(ctx, nil, nil)
	}

	return reply(ctx, nil, nil)
}

// handleSelectorExpr returns jsonrpc2.Replier for Hover
// on SelectorExpr
// TODO: Move duplicate logic here
// func handleSelectorExpr(expr *ast.SelectorExpr) jsonrpc2.Replier {
// }
