package lsp

import (
	"context"
	"fmt"

	"go.lsp.dev/jsonrpc2"
)

func sendParseError(ctx context.Context, reply jsonrpc2.Replier, err error) error {
	return reply(ctx, nil, fmt.Errorf("%w: %s", jsonrpc2.ErrParse, err))
}
