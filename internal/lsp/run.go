package lsp

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/harry-hov/gnopls/internal/env"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/pkg/fakenet"
)

func RunServer(ctx context.Context, env *env.Env) error {
	conn := jsonrpc2.NewConn(jsonrpc2.NewStream(fakenet.NewConn("stdio", os.Stdin, os.Stdout)))
	handler := BuildServerHandler(conn, env)
	stream := jsonrpc2.HandlerServer(handler)
	err := stream.ServeStream(ctx, conn)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
