package main

import (
	"os"
	"runtime"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	// pin the main goroutine to the main os thread. glfw, and libdecor's gtk plugin with it, must initialize and run
	// on that thread; unpinned, the goroutine can migrate before the window is created, gtk_init_check fails, and
	// wayland windows come up without decorations, depending on scheduling. the lock only helps because ui.Run
	// creates and runs the window on this same goroutine.
	runtime.LockOSThread()
	dl.Init(dl.DefaultOptions().SetTrimPrefix("github.com/michaelquigley/"))
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var opts ui.Options
	cmd := &cobra.Command{
		Use:          "patchbay",
		Short:        "a pipewire patchbay",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// cobra runs this on the main goroutine, pinned to the main os thread in init.
			return ui.Run(opts)
		},
	}
	cmd.Flags().StringVar(&opts.Sample, "sample", "", "run against a captured sample directory, read-only")
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "workspace file (default ~/.config/patchbay/workspace.yaml; sample-workspace.yaml beside it in sample mode)")
	cmd.AddCommand(newDumpCmd())
	return cmd
}
