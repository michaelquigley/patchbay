package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/sample"
	"github.com/spf13/cobra"
)

func newDumpCmd() *cobra.Command {
	var sampleDir string
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "watch the observed graph keyed by serial",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sampleDir != "" {
				snap, err := sample.Load(sampleDir)
				if err != nil {
					return err
				}
				printSnapshot(cmd.OutOrStdout(), snap)
				return nil
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return dumpLive(ctx, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&sampleDir, "sample", "", "print a captured sample directory instead of the live graph")
	return cmd
}

// dumpLive polls every 50 ms, printing observed connection changes and changed live snapshots.
func dumpLive(ctx context.Context, w io.Writer) error {
	conn := pipewire.Connect()
	defer conn.Close()

	var last *pipewire.Snapshot
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		snap := conn.Snapshot()
		if snap == last {
			continue
		}
		if last == nil || snap.State != last.State || snap.Error != last.Error {
			stateLine(w, snap)
		}
		if snap.State == pipewire.Live {
			printSnapshot(w, snap)
		}
		last = snap
	}
}

func stateLine(w io.Writer, s *pipewire.Snapshot) {
	if s.Error != "" {
		fmt.Fprintf(w, "# %v: %v (%v)\n", time.Now().Format(time.TimeOnly), s.State, s.Error)
		return
	}
	fmt.Fprintf(w, "# %v: %v\n", time.Now().Format(time.TimeOnly), s.State)
}

// unresolvedNote names references left unresolved at announcement, so a missing cable reads as unknown rather than
// absent.
func unresolvedNote(s *pipewire.Snapshot) string {
	switch s.Unresolved {
	case 0:
		return ""
	case 1:
		return " · 1 unresolved reference"
	default:
		return fmt.Sprintf(" · %d unresolved references", s.Unresolved)
	}
}

func sortedKeys[V any](m map[pipewire.Serial]V) []pipewire.Serial {
	keys := make([]pipewire.Serial, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func portLabel(s *pipewire.Snapshot, serial pipewire.Serial) string {
	p, ok := s.Ports[serial]
	if !ok {
		return "?"
	}
	if p.Alias != "" {
		return p.Alias
	}
	return p.Name
}

func printSnapshot(w io.Writer, s *pipewire.Snapshot) {
	fmt.Fprintf(w, "--- generation %d · %v%s · %d nodes · %d ports · %d links · %d devices · %d clients\n",
		s.Generation, s.State, unresolvedNote(s), len(s.Nodes), len(s.Ports), len(s.Links), len(s.Devices), len(s.Clients))
	st := s.Settings
	fmt.Fprintf(w, "settings: rate %d · quantum %d (min %d, max %d) · force-quantum %d · force-rate %d\n",
		st.Rate, st.Quantum, st.MinQuantum, st.MaxQuantum, st.ForceQuantum, st.ForceRate)

	portsByNode := map[pipewire.Serial][]pipewire.Serial{}
	for _, serial := range sortedKeys(s.Ports) {
		p := s.Ports[serial]
		portsByNode[p.NodeSerial] = append(portsByNode[p.NodeSerial], serial)
	}
	for _, serial := range sortedKeys(s.Nodes) {
		n := s.Nodes[serial]
		class := n.MediaClass
		if class == "" {
			class = "-"
		}
		fmt.Fprintf(w, "node %d [id %d] %s · %s · %s\n", serial, n.ID, n.Name, class, n.State)
		for _, ps := range portsByNode[serial] {
			p := s.Ports[ps]
			monitor := ""
			if p.Monitor {
				monitor = " monitor"
			}
			fmt.Fprintf(w, "  port %d [id %d] %v %v%s %s\n", ps, p.ID, p.Direction, p.Media, monitor, portLabel(s, ps))
		}
	}
	if orphans := portsByNode[0]; len(orphans) > 0 {
		fmt.Fprintf(w, "ports without an observed node:\n")
		for _, ps := range orphans {
			fmt.Fprintf(w, "  port %d %s\n", ps, portLabel(s, ps))
		}
	}
	for _, serial := range sortedKeys(s.Links) {
		l := s.Links[serial]
		fmt.Fprintf(w, "link %d [id %d] %d (%s) -> %d (%s) · %s\n",
			serial, l.ID, l.OutPort, portLabel(s, l.OutPort), l.InPort, portLabel(s, l.InPort), l.State)
	}
	for _, serial := range sortedKeys(s.Devices) {
		d := s.Devices[serial]
		fmt.Fprintf(w, "device %d [id %d] %s\n", serial, d.ID, d.Props["device.name"])
	}
	for _, serial := range sortedKeys(s.Clients) {
		c := s.Clients[serial]
		fmt.Fprintf(w, "client %d [id %d] %s\n", serial, c.ID, c.Props["application.name"])
	}
}
