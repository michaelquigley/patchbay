package main

import (
	"os"

	"github.com/michaelquigley/df/dl"
	"github.com/spf13/cobra"
)

func init() {
	dl.Init(dl.DefaultOptions().SetTrimPrefix("github.com/michaelquigley/"))
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "patchbay",
		Short:        "a pipewire patchbay",
		SilenceUsage: true,
	}
	cmd.AddCommand(newDumpCmd())
	return cmd
}
