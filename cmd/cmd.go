package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"github.com/spf13/cobra"
)

func GnoplsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "gnopls",
		Short:              `Gno Please! is a Gno language server`,
		DisableSuggestions: true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Info("Initializing Server...")

			return nil
		},
	}

	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.AddCommand(CmdServe())
	cmd.AddCommand(CmdVersion())

	return cmd
}

func Execute() {
	if err := GnoplsCmd().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
