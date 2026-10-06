package pipewire

// Replay folds a complete, already-enumerated set of inputs into a live snapshot without a daemon. it is how a
// captured graph enters the same model the live backend feeds: every sync the graph asks for is answered after the
// inputs have been applied, so the barrier passes exactly as it would at the end of a live enumeration.
func Replay(inputs []Input) *Snapshot {
	drv := &replayDriver{}
	g := newGraph(drv)
	g.connection = 1
	g.start()
	for _, in := range inputs {
		g.Apply(in)
	}
	for i := 0; i < 2 && g.phase != barrierPassed; i++ {
		g.Apply(SyncDone{Seq: g.syncSeq})
	}
	snap := g.fold()
	snap.Generation = 1
	return snap
}

type replayDriver struct {
	seq int
}

func (d *replayDriver) bind(uint32, string, uint32, Serial) bool { return true }
func (d *replayDriver) unbind(Serial)                            {}

// a replayed capture is read-only: it refuses to create anything, and destroys nothing.
func (d *replayDriver) createLink(uint32, uint32, uint32, uint32, RequestID) bool { return false }
func (d *replayDriver) releaseLink(RequestID)                                     {}
func (d *replayDriver) destroyGlobal(uint32)                                      {}
func (d *replayDriver) sync() int {
	d.seq++
	return d.seq
}
