package cmd

import (
	"fmt"

	"github.com/harry-hov/gnopls/internal/version"
	"github.com/spf13/cobra"
)

func CmdVersion() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the gnopls version information",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(version.GetVersion(cmd.Context()))
			return nil
		},
	}

	return cmd
}
