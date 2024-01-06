package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func (s *server) Definition(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DefinitionParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	file, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}

	offset := file.PositionToOffset(params.Position)
	slog.Info("definition", "offset", offset)

	pgf, err := file.ParseGno(ctx)
	if err != nil {
		return reply(ctx, nil, errors.New("cannot parse gno file"))
	}

	for _, spec := range pgf.File.Imports {
		slog.Info("definition", "spec", spec.Path.Value, "pos", spec.Path.Pos(), "end", spec.Path.End())
		if int(spec.Path.Pos()) <= offset && offset <= int(spec.Path.End()) {
			// TODO: handle definition for imports
			slog.Info("definition", "import", spec.Path.Value)
			return reply(ctx, nil, nil)
		}
	}

	token, err := file.TokenAt(params.Position)
	if err != nil {
		return reply(ctx, protocol.Hover{}, err)
	}
	text := strings.TrimSpace(token.Text)

	// FIXME: Use the AST package to do this + get type of token.
	//
	// This is just a quick PoC to get something working.

	// strings.Split(p.Body,
	text = strings.Split(text, "(")[0]

	text = strings.TrimSuffix(text, ",")
	text = strings.TrimSuffix(text, ")")

	// *mux.Request
	text = strings.TrimPrefix(text, "*")

	slog.Info("definition", "pkg", len(s.completionStore.pkgs))

	parts := strings.Split(text, ".")
	if len(parts) == 2 {
		pkg := parts[0]
		sym := parts[1]

		slog.Info("definition", "pkg", pkg, "sym", sym)
		symbol := s.completionStore.lookupSymbol(pkg, sym)
		if symbol != nil {
			slog.Info("definition", "URI", symbol.FileURI)
			return reply(ctx, protocol.Location{
				URI: symbol.FileURI,
				Range: *posToRange(
					symbol.Position.Line,
					[]int{0, 0},
				),
			}, nil)
		}
	}

	return reply(ctx, nil, nil)
}
