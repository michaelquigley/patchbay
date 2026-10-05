//go:build live

package pipewire

import (
	"testing"
	"time"
)

// TestLiveObservation connects to the user's daemon and waits for initial observation to complete. it observes only;
// nothing is created, destroyed, or set. run with `go test -tags live ./internal/pipewire`.
func TestLiveObservation(t *testing.T) {
	conn := Connect()
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := conn.Snapshot()
		if s.State == Live {
			if len(s.Nodes) == 0 || !s.Settings.Present || s.Settings.Rate == 0 {
				t.Fatalf("live snapshot without nodes or settings: %d nodes, settings %+v", len(s.Nodes), s.Settings)
			}
			for serial, p := range s.Ports {
				if p.NodeSerial == 0 || p.Direction == DirectionUnknown {
					t.Errorf("port %d unresolved: %+v", serial, p)
				}
			}
			for serial, n := range s.Nodes {
				if n.Props["device.id"] != "" && n.DeviceSerial == 0 {
					t.Errorf("node %d names device %v, unresolved", serial, n.Props["device.id"])
				}
			}
			for serial, l := range s.Links {
				if l.OutPort == 0 || l.InPort == 0 || l.State == "" {
					t.Errorf("link %d unresolved: %+v", serial, l)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never reached live; state %v (%v)", conn.Snapshot().State, conn.Snapshot().Error)
}
