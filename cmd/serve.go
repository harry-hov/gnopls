package cmd

import (
	"log/slog"
	"os"

	"github.com/harry-hov/gnopls/internal/env"
	"github.com/harry-hov/gnopls/internal/lsp"
	"github.com/spf13/cobra"
)

func CmdServe() *cobra.Command {
	var gnoroot string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a server for Gno code using the Language Server Protocol",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Info("Initializing Server...")
			env := &env.Env{
				GNOROOT: gnoroot,
				GNOHOME: env.GnoHome(),
			}
			if env.GNOROOT == "" {
				env.GNOROOT = os.Getenv("GNOROOT")
			}
			err := lsp.RunServer(cmd.Context(), env)
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&gnoroot, "gnoroot", "", "", "specify the GNOROOT")

	return cmd
}
