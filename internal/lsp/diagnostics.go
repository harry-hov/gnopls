package lsp

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func (s *server) publishDiagnostics(ctx context.Context, conn jsonrpc2.Conn, file *GnoFile) error {
	slog.Info("Lint", "path", file.URI.Filename())

	errors, err := s.TranspileAndBuild(file)
	if err != nil {
		return err
	}

	mPublishDiagnosticParams := make(map[string]*protocol.PublishDiagnosticsParams)
	publishDiagnosticParams := make([]*protocol.PublishDiagnosticsParams, 0)
	for _, er := range errors {
		if !strings.HasSuffix(file.URI.Filename(), er.FileName) {
			continue
		}
		diagnostic := protocol.Diagnostic{
			Range:    *posToRange(er.Line, er.Span),
			Severity: protocol.DiagnosticSeverityError,
			Source:   "gnopls",
			Message:  er.Msg,
			Code:     er.Tool,
		}
		if pdp, ok := mPublishDiagnosticParams[er.FileName]; ok {
			pdp.Diagnostics = append(pdp.Diagnostics, diagnostic)
			continue
		}
		publishDiagnosticParam := protocol.PublishDiagnosticsParams{
			URI:         file.URI,
			Diagnostics: []protocol.Diagnostic{diagnostic},
		}
		publishDiagnosticParams = append(publishDiagnosticParams, &publishDiagnosticParam)
		mPublishDiagnosticParams[er.FileName] = &publishDiagnosticParam
	}

	// Clean old diagnosed errors if no error found for current file
	found := false
	for _, er := range errors {
		if strings.HasSuffix(er.FileName, filepath.Base(file.URI.Filename())) {
			found = true
			break
		}
	}
	if !found {
		publishDiagnosticParams = append(publishDiagnosticParams, &protocol.PublishDiagnosticsParams{
			URI:         file.URI,
			Diagnostics: []protocol.Diagnostic{},
		})
	}

	return conn.Notify(
		ctx,
		protocol.MethodTextDocumentPublishDiagnostics,
		publishDiagnosticParams,
	)
}
