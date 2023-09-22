package lsp

import "go/token"

// A Selection represents the cursor position and surrounding identifier.
type Selection struct {
	content            string
	tokFile            *token.File
	start, end, cursor token.Pos
}
