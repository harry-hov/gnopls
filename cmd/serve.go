package cmd

import (
	"log/slog"
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

			return nil
		},
	}

	cmd.Flags().StringVarP(&gnoroot, "gnoroot", "", "", "specify the GNOROOT")

	return cmd
}
