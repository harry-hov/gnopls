package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func (s *server) DidOpen(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	file := &GnoFile{
		URI: uri,
		Src: []byte(params.TextDocument.Text),
	}
	s.snapshot.file.Set(uri.Filename(), file)

	slog.Info("open " + string(params.TextDocument.URI.Filename()))
	s.UpdateCache(filepath.Dir(string(params.TextDocument.URI.Filename())))
	notification := s.publishDiagnostics(ctx, s.conn, file)
	return reply(ctx, notification, nil)
}

func (s *server) DidClose(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	slog.Info("close" + string(params.TextDocument.URI.Filename()))
	return reply(ctx, s.conn.Notify(ctx, protocol.MethodTextDocumentDidClose, nil), nil)
}

func (s *server) DidChange(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	_, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}

	file := &GnoFile{
		URI: uri,
		Src: []byte(params.ContentChanges[0].Text),
	}
	s.snapshot.file.Set(uri.Filename(), file)

	slog.Info("change " + string(params.TextDocument.URI.Filename()))
	return reply(ctx, nil, nil)
}

func (s *server) DidSave(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DidSaveTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return sendParseError(ctx, reply, err)
	}

	uri := params.TextDocument.URI
	file, ok := s.snapshot.Get(uri.Filename())
	if !ok {
		return reply(ctx, nil, errors.New("snapshot not found"))
	}

	slog.Info("save " + string(uri.Filename()))
	notification := s.publishDiagnostics(ctx, s.conn, file)
	return reply(ctx, notification, nil)
}
