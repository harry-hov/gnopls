package tools

import (
	"errors"
	"go/format"

	gofumpt "mvdan.cc/gofumpt/format"
)

type FormattingOption int

const (
	Gofmt FormattingOption = iota
	Gofumpt
)

func Format(data string, opt FormattingOption) ([]byte, error) {
	switch opt {
	case Gofmt:
		return RunGofmt(data)
	case Gofumpt:
		return RunGofumpt(data)
	default:
		return nil, errors.New("gnopls: invalid formatting option")
	}
}

func RunGofmt(data string) ([]byte, error) {
	return format.Source([]byte(data))
}

func RunGofumpt(data string) ([]byte, error) {
	return gofumpt.Source([]byte(data), gofumpt.Options{})
}
