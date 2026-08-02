package cli

import "github.com/spf13/cobra"

func newVersionCmd(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print champs version metadata",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("champs " + info.String())
		},
	}
}
