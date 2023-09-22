package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"

	"github.com/harry-hov/gnopls/internal/tools"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func (s *server) Formatting(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DocumentFormattingParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	file, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}

	formatted, err := tools.Format(string(file.Src), s.formatOpt)
	if err != nil {
		return reply(ctx, nil, err)
	}

	slog.Info("format " + string(params.TextDocument.URI.Filename()))
	return reply(ctx, []protocol.TextEdit{
		{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End: protocol.Position{
					Line:      math.MaxInt32,
					Character: math.MaxInt32,
				},
			},
			NewText: string(formatted),
		},
	}, nil)
}
