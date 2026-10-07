package pipewire

import (
	"sort"
	"strconv"
	"time"

	"github.com/michaelquigley/df/dl"
)

const (
	typeNode     = "PipeWire:Interface:Node"
	typePort     = "PipeWire:Interface:Port"
	typeLink     = "PipeWire:Interface:Link"
	typeDevice   = "PipeWire:Interface:Device"
	typeClient   = "PipeWire:Interface:Client"
	typeMetadata = "PipeWire:Interface:Metadata"
	typeProfiler = "PipeWire:Interface:Profiler"

	metadataSettings = "settings"
	metadataDefault  = "default"
)

// Input is one observation delivered to a Graph: a registry or bound-object callback, translated out of native types.
// the native transport produces inputs on the loop thread; tests and the sample loader produce them directly.
type Input interface {
	input()
}

// GlobalAdded is a registry global announcement.
type GlobalAdded struct {
	ID      uint32
	Type    string
	Version uint32
	Props   map[string]string
}

// GlobalRemoved is a registry global removal.
type GlobalRemoved struct {
	ID uint32
}

// NodeInfo is a bound node's info event. nil Props means the props did not change.
type NodeInfo struct {
	Serial Serial
	State  string
	Error  string
	Props  map[string]string
}

// PortInfo is a bound port's info event. nil Props means the props did not change.
type PortInfo struct {
	Serial    Serial
	Direction Direction
	Props     map[string]string
}

// LinkInfo is a bound link's info event. nil Props means the props did not change. its endpoint ids are not carried:
// endpoints are resolved from the registry announcement only, while the ids still name the link's ports.
type LinkInfo struct {
	Serial Serial
	State  string
	Error  string
	Props  map[string]string
}

// ObjectInfo is a bound device's or client's info event. nil Props means the props did not change.
type ObjectInfo struct {
	Serial Serial
	Props  map[string]string
}

// MetadataProperty is a bound metadata object's property event. an empty Key removes every property of the subject;
// Removed removes the one key.
type MetadataProperty struct {
	Serial  Serial
	Subject uint32
	Key     string
	Type    string
	Value   string
	Removed bool
}

// ProxyError reports an error on a bound object's proxy. one that arrives before the first info (a refused bind)
// would otherwise hold the sync barrier forever.
type ProxyError struct {
	Serial Serial
	Error  string
}

// SyncDone is a core done event for a sync the graph requested.
type SyncDone struct {
	Seq int
}

func (GlobalAdded) input()      {}
func (GlobalRemoved) input()    {}
func (NodeInfo) input()         {}
func (PortInfo) input()         {}
func (LinkInfo) input()         {}
func (ObjectInfo) input()       {}
func (MetadataProperty) input() {}
func (ProxyError) input()       {}
func (SyncDone) input()         {}

// driver is what a Graph asks of its transport. calls happen on the thread that applies inputs.
type driver interface {
	// bind binds the global and routes its info and property events back as inputs carrying serial.
	bind(id uint32, typ string, version uint32, serial Serial) bool
	// unbind releases the binding made for serial.
	unbind(serial Serial)
	// sync issues a core sync round trip and returns its sequence number.
	sync() int
	// createLink asks the link factory for a lingering link between the ports, routing the proxy's bound, error,
	// and removed events back as inputs carrying req.
	createLink(outNode, outPort, inNode, inPort uint32, req RequestID) bool
	// releaseLink drops the proxy made for req; a lingering link outlives it.
	releaseLink(req RequestID)
	// destroyGlobal asks the server to destroy the global with this id.
	destroyGlobal(id uint32)
	// setMetadata sets a property on the bound metadata object with this serial.
	setMetadata(serial Serial, subject uint32, key, typ, value string) bool
	// monotonicNow reads CLOCK_MONOTONIC, the clock profiler pods carry.
	monotonicNow() int64
}

type barrierPhase int

const (
	awaitingFirstSync barrierPhase = iota
	awaitingSecondSync
	barrierPassed
)

type object struct {
	kind    ObjectKind
	id      uint32
	serial  Serial
	props   map[string]string
	hasInfo bool

	// node and link state
	state string
	err   string

	// node; device is zero when the node names no device or the device was not observed at the node's announcement.
	// appeared is the backend's CLOCK_MONOTONIC reading when the announcement was received: profiler pods stamped
	// earlier belong to a departed node that held the id.
	device   Serial
	appeared int64

	// port; node is zero when the owner was not observed at the port's announcement
	direction Direction
	nodeID    uint32
	node      Serial

	// link; an endpoint is zero when it was not observed at the link's announcement
	outPort, inPort Serial
	outNode, inNode Serial

	// metadata
	metadataName string
	entries      map[uint32]map[string]MetadataEntry // each entry carries its own resolved subject serial
	pending      map[uint32]map[string]bool          // entries delivered during enumeration, resolved at the latch
}

// sessionState is everything the backend knows only for the lifetime of one connection. serials come from a
// per-daemon counter, so a restarted daemon can reuse numbers seen before; this state is therefore cleared explicitly
// on disconnect rather than trusted to miss on lookup.
type sessionState struct {
	pending     map[RequestID]*linkRequest
	createdHere map[Serial]struct{} // links this process created, by the serial of the one link lifetime confirmed
	metrics     map[Serial]*metricsRecord
	drivers     map[Serial]*driverRecord
}

func newSessionState() sessionState {
	return sessionState{
		pending:     map[RequestID]*linkRequest{},
		createdHere: map[Serial]struct{}{},
		metrics:     map[Serial]*metricsRecord{},
		drivers:     map[Serial]*driverRecord{},
	}
}

// Graph folds inputs from one connection into snapshots. it is not safe for concurrent use; the native transport
// drives it from the loop thread only.
type Graph struct {
	connection uint64 // the connection this graph observes; stamped on every snapshot it folds as Session
	drv        driver
	objects    map[Serial]*object
	byID       map[uint32]Serial

	phase       barrierPhase
	syncSeq     int
	pendingInfo map[Serial]struct{} // objects announced before the barrier latched, still owing their first info
	latched     bool                // initial observation completed; stays true for the graph's lifetime

	session  sessionState
	resolved []Request // recent resolved requests, oldest first; kept with the graph, not the session state
	// maxSerial is the highest serial announced so far: serials are monotonic within a daemon, so a link a request
	// creates is announced above the watermark taken when the request was posted.
	maxSerial Serial
	now       func() time.Time

	// metrics: the last published summary, when it was computed, whether pods arrived since, and how many
	// summaries have changed (for the rate test). the profiler global is bound once.
	summary      MetricsSummary
	lastSummary  time.Time
	metricsDirty bool
	summaries    int
	profiler     Serial

	events []Event
	dirty  bool
}

func newGraph(drv driver) *Graph {
	g := &Graph{
		drv:         drv,
		objects:     map[Serial]*object{},
		byID:        map[uint32]Serial{},
		pendingInfo: map[Serial]struct{}{},
		session:     newSessionState(),
		now:         time.Now,
		dirty:       true,
	}
	return g
}

// start issues the first sync round trip. it must be called after the registry listener is attached.
func (g *Graph) start() {
	g.phase = awaitingFirstSync
	g.syncSeq = g.drv.sync()
}

// live reports whether initial observation is complete. it latches: once true it stays true until the connection is
// lost and a new graph is built, so objects announced afterwards never send the state back to connecting.
func (g *Graph) live() bool {
	return g.latched
}

// Apply folds one input into the graph's mutable state.
func (g *Graph) Apply(in Input) {
	switch in := in.(type) {
	case GlobalAdded:
		g.globalAdded(in)
	case GlobalRemoved:
		g.globalRemoved(in.ID)
	case NodeInfo:
		if o := g.info(in.Serial, KindNode); o != nil {
			o.state, o.err = in.State, in.Error
			if in.Props != nil {
				o.props = in.Props
			}
		}
	case PortInfo:
		if o := g.info(in.Serial, KindPort); o != nil {
			if in.Direction != DirectionUnknown {
				o.direction = in.Direction
			}
			if in.Props != nil {
				o.props = in.Props
			}
		}
	case LinkInfo:
		if o := g.objects[in.Serial]; o != nil && o.kind == KindLink {
			first := !o.hasInfo
			g.markInfo(o)
			if in.Props != nil {
				o.props = in.Props
			}
			if first || o.state != in.State || o.err != in.Error {
				o.state, o.err = in.State, in.Error
				g.events = append(g.events, LinkStateChanged{Serial: o.serial, State: in.State, Error: in.Error})
				g.linkState(o)
			}
		}
	case ObjectInfo:
		if o := g.objects[in.Serial]; o != nil && (o.kind == KindDevice || o.kind == KindClient) {
			g.markInfo(o)
			if in.Props != nil {
				o.props = in.Props
			}
		}
	case MetadataProperty:
		g.metadataProperty(in)
	case ProxyError:
		if o := g.objects[in.Serial]; o != nil {
			dl.Warnf("proxy error on %v %d (serial %d): %v", o.kind, o.id, o.serial, in.Error)
			delete(g.pendingInfo, in.Serial)
		}
	case SyncDone:
		g.syncDone(in.Seq)
	case RequestPosted:
		g.postRequest(in)
	case LinkBound:
		g.linkBound(in)
	case LinkProxyError:
		g.linkProxyError(in)
	case LinkProxyRemoved:
		g.linkProxyRemoved(in)
	case Tick:
		g.expire()
		g.publishMetrics()
		return
	case ProfilePoint:
		g.profile(in)
		return
	case ResetBaseline:
		g.resetBaseline()
		return
	}
	g.dirty = true
	if !g.latched && g.phase == barrierPassed && len(g.pendingInfo) == 0 {
		g.latched = true
		g.pendingInfo = nil
		g.resolvePendingSubjects()
		g.events = append(g.events, ConnStateChanged{State: Live})
	}
}

func (g *Graph) info(serial Serial, kind ObjectKind) *object {
	o := g.objects[serial]
	if o == nil || o.kind != kind {
		return nil
	}
	g.markInfo(o)
	return o
}

func (g *Graph) markInfo(o *object) {
	o.hasInfo = true
	delete(g.pendingInfo, o.serial)
}

func kindOf(typ string) (ObjectKind, bool) {
	switch typ {
	case typeProfiler:
		return kindProfiler, true
	case typeNode:
		return KindNode, true
	case typePort:
		return KindPort, true
	case typeLink:
		return KindLink, true
	case typeDevice:
		return KindDevice, true
	case typeClient:
		return KindClient, true
	case typeMetadata:
		return KindMetadata, true
	}
	return 0, false
}

func (g *Graph) globalAdded(in GlobalAdded) {
	kind, ok := kindOf(in.Type)
	if !ok {
		return
	}
	if kind == KindMetadata {
		name := in.Props["metadata.name"]
		if name != metadataSettings && name != metadataDefault {
			return
		}
	}
	if kind == kindProfiler && g.profiler != 0 {
		return // bound once
	}
	serial, err := strconv.ParseUint(in.Props["object.serial"], 10, 64)
	if err != nil || serial == 0 {
		dl.Warnf("ignoring %v %d without a usable object.serial ('%s')", kind, in.ID, in.Props["object.serial"])
		return
	}
	if prior, found := g.byID[in.ID]; found {
		dl.Warnf("id %d announced again (serial %d replaces %d) without a removal", in.ID, serial, prior)
		g.globalRemoved(in.ID)
	}
	if _, found := g.objects[Serial(serial)]; found {
		dl.Warnf("serial %d announced twice; ignoring the second announcement (id %d)", serial, in.ID)
		return
	}

	if Serial(serial) > g.maxSerial {
		g.maxSerial = Serial(serial)
	}
	o := &object{kind: kind, id: in.ID, serial: Serial(serial), props: in.Props}
	switch kind {
	case KindNode:
		o.appeared = g.drv.monotonicNow()
		if id, ok := in.Props["device.id"]; ok {
			o.device = g.serialOf(propUint32(in.Props, "device.id"), KindDevice)
			if o.device == 0 {
				dl.Warnf("node %d (serial %d) names device id '%s', which is not observed; device left unresolved", o.id, o.serial, id)
			}
		}
	case KindPort:
		o.direction = directionOf(in.Props["port.direction"])
		o.nodeID = propUint32(in.Props, "node.id")
		o.node = g.serialOf(o.nodeID, KindNode)
		if o.node == 0 {
			dl.Warnf("port %d (serial %d) names node id %d, which is not observed; owner left unresolved", o.id, o.serial, o.nodeID)
		}
	case KindLink:
		outPortID, inPortID := propUint32(in.Props, "link.output.port"), propUint32(in.Props, "link.input.port")
		outNodeID, inNodeID := propUint32(in.Props, "link.output.node"), propUint32(in.Props, "link.input.node")
		o.outPort, o.inPort = g.serialOf(outPortID, KindPort), g.serialOf(inPortID, KindPort)
		o.outNode, o.inNode = g.serialOf(outNodeID, KindNode), g.serialOf(inNodeID, KindNode)
		if !o.linkResolved() {
			dl.Warnf("link %d (serial %d) names ports %d -> %d on nodes %d -> %d, not all observed; left unresolved",
				o.id, o.serial, outPortID, inPortID, outNodeID, inNodeID)
		}
	case KindMetadata:
		o.metadataName = in.Props["metadata.name"]
		o.entries = map[uint32]map[string]MetadataEntry{}
		o.pending = map[uint32]map[string]bool{}
	}
	g.objects[o.serial] = o
	g.byID[o.id] = o.serial

	if g.drv.bind(in.ID, in.Type, in.Version, o.serial) {
		// metadata and the profiler have no info event; metadata properties arrive on bind and are covered by the
		// second sync. objects announced after the barrier latched track first info on the object alone and never
		// hold the state.
		if kind == kindProfiler {
			g.profiler = o.serial
			g.metricsDirty = true
		}
		if kind != KindMetadata && kind != kindProfiler && !g.latched {
			g.pendingInfo[o.serial] = struct{}{}
		}
	} else {
		dl.Warnf("could not bind %v %d (serial %d)", kind, in.ID, o.serial)
	}
	if kind != KindMetadata && kind != kindProfiler {
		g.events = append(g.events, ObjectAppeared{Kind: kind, Serial: o.serial})
	}
	if kind == KindLink {
		g.linkAnnounced(o)
	}
}

func (g *Graph) globalRemoved(id uint32) {
	serial, found := g.byID[id]
	if !found {
		return
	}
	o := g.objects[serial]
	delete(g.byID, id)
	delete(g.objects, serial)
	delete(g.pendingInfo, serial)
	g.drv.unbind(serial)
	if _, ok := g.session.metrics[serial]; ok {
		delete(g.session.metrics, serial)
		g.metricsDirty = true
	}
	if _, ok := g.session.drivers[serial]; ok {
		delete(g.session.drivers, serial)
		g.metricsDirty = true
	}
	if o.kind == kindProfiler {
		g.profiler = 0
		g.metricsDirty = true
	}
	if o.kind != KindMetadata && o.kind != kindProfiler {
		g.events = append(g.events, ObjectVanished{Kind: o.kind, Serial: serial})
	}
	if o.kind == KindLink {
		g.linkRemoved(serial)
	}
	g.orphanSubjects(id, serial)
}

// resolvePendingSubjects resolves the subjects of entries delivered during initial enumeration, once, against the
// enumerated graph.
func (g *Graph) resolvePendingSubjects() {
	for _, o := range g.objects {
		if o.kind != KindMetadata {
			continue
		}
		for id, keys := range o.pending {
			for key := range keys {
				if e, ok := o.entries[id][key]; ok {
					e.SubjectSerial = g.byID[id]
					o.entries[id][key] = e
				}
			}
		}
		o.pending = map[uint32]map[string]bool{}
	}
}

// orphanSubjects unresolves the metadata entries that named a removed object: those resolved to its serial, and
// those still waiting for the barrier under its id. they stay unresolved; a later holder of the id never inherits
// them.
func (g *Graph) orphanSubjects(id uint32, serial Serial) {
	for _, o := range g.objects {
		if o.kind != KindMetadata {
			continue
		}
		for subject, keys := range o.entries {
			for key, e := range keys {
				if e.SubjectSerial == serial {
					e.SubjectSerial = 0
					keys[key] = e
					g.dirty = true
				}
			}
			if subject == id && len(o.pending[id]) > 0 {
				delete(o.pending, id)
				g.dirty = true
			}
		}
	}
}

func (o *object) linkResolved() bool {
	return o.outPort != 0 && o.inPort != 0 && o.outNode != 0 && o.inNode != 0
}

// serialOf resolves a protocol id to the serial of the object holding it now. it is called only while an object is
// being announced: references are resolved at that moment or never, because an id that misses now may later be held
// by a different object.
func (g *Graph) serialOf(id uint32, kind ObjectKind) Serial {
	if s, ok := g.byID[id]; ok && g.objects[s].kind == kind {
		return s
	}
	return 0
}

func (g *Graph) metadataProperty(in MetadataProperty) {
	o := g.objects[in.Serial]
	if o == nil || o.kind != KindMetadata {
		return
	}
	if in.Key == "" {
		delete(o.entries, in.Subject)
		delete(o.pending, in.Subject)
		return
	}
	subject := o.entries[in.Subject]
	if in.Removed {
		delete(subject, in.Key)
		delete(o.pending[in.Subject], in.Key)
		return
	}
	if subject == nil {
		subject = map[string]MetadataEntry{}
		o.entries[in.Subject] = subject
	}
	e := MetadataEntry{Subject: in.Subject, Key: in.Key, Type: in.Type, Value: in.Value}
	delete(o.pending[in.Subject], in.Key)
	if in.Subject != 0 {
		// each arrival resolves only itself: now, against the object holding the id, or, during initial
		// enumeration, when the barrier latches and the enumerated graph is complete.
		if g.latched {
			e.SubjectSerial = g.byID[in.Subject]
		} else {
			if o.pending[in.Subject] == nil {
				o.pending[in.Subject] = map[string]bool{}
			}
			o.pending[in.Subject][in.Key] = true
		}
	}
	subject[in.Key] = e
	g.settingsEchoed(in.Serial, in)
}

// syncDone advances the barrier. the first round trip ends enumeration; a second is issued so the bind requests made
// while enumerating have been answered (info, metadata properties) before observation is declared complete.
func (g *Graph) syncDone(seq int) {
	if seq != g.syncSeq {
		return
	}
	switch g.phase {
	case awaitingFirstSync:
		g.phase = awaitingSecondSync
		g.syncSeq = g.drv.sync()
	case awaitingSecondSync:
		g.phase = barrierPassed
		if len(g.pendingInfo) > 0 {
			dl.Infof("barrier waiting on first info from %d objects", len(g.pendingInfo))
		}
	}
}

// takeEvents returns and clears the events queued since the last call.
func (g *Graph) takeEvents() []Event {
	ev := g.events
	g.events = nil
	return ev
}

// fold builds an immutable snapshot of the current state. it returns nil when nothing changed since the last fold.
func (g *Graph) fold() *Snapshot {
	if !g.dirty {
		return nil
	}
	g.dirty = false

	state := Connecting
	if g.live() {
		state = Live
	}
	s := emptySnapshot(state)
	s.Session = g.connection
	for _, o := range g.objects {
		switch o.kind {
		case KindNode:
			_, hasDevice := o.props["device.id"]
			if hasDevice && o.device == 0 {
				s.Unresolved++
			}
			s.Nodes[o.serial] = Node{
				Serial:       o.serial,
				ID:           o.id,
				Props:        o.props,
				Name:         o.props["node.name"],
				AppName:      o.props["application.name"],
				Description:  o.props["node.description"],
				Nick:         o.props["node.nick"],
				MediaClass:   o.props["media.class"],
				HasDevice:    hasDevice,
				DeviceSerial: o.device,
				State:        o.state,
				Error:        o.err,
			}
		case KindDevice:
			card, err := strconv.Atoi(o.props["api.alsa.card"])
			s.Devices[o.serial] = Device{Serial: o.serial, ID: o.id, Props: o.props, HardwareSerial: o.props["device.serial"],
				ALSACard: card, HasALSACard: err == nil && card >= 0}
		case KindClient:
			s.Clients[o.serial] = Client{Serial: o.serial, ID: o.id, Props: o.props}
		case KindLink:
			if !o.linkResolved() {
				s.Unresolved++
			}
			s.Links[o.serial] = Link{
				Serial:  o.serial,
				ID:      o.id,
				Props:   o.props,
				OutPort: o.outPort,
				InPort:  o.inPort,
				OutNode: o.outNode,
				InNode:  o.inNode,
				State:   o.state,
				Error:   o.err,
			}
			if _, ok := g.session.createdHere[o.serial]; ok {
				l := s.Links[o.serial]
				l.CreatedHere = true
				s.Links[o.serial] = l
			}
		case KindMetadata:
			switch o.metadataName {
			case metadataSettings:
				s.Settings = decodeSettings(o.entries[0])
			case metadataDefault:
				s.Default = flattenEntries(o.entries)
			}
		}
	}
	s.Requests = g.requests()
	s.Metrics = g.summary
	for _, o := range g.objects {
		if o.kind != KindPort {
			continue
		}
		if o.node == 0 {
			s.Unresolved++
		}
		nodeClass := ""
		if n, ok := s.Nodes[o.node]; ok {
			nodeClass = n.MediaClass
		}
		s.Ports[o.serial] = Port{
			Serial:      o.serial,
			ID:          o.id,
			Props:       o.props,
			NodeSerial:  o.node,
			NodeID:      o.nodeID,
			Direction:   o.direction,
			Media:       mediaOf(o.props["format.dsp"], nodeClass),
			Monitor:     o.props["port.monitor"] == "true",
			Name:        o.props["port.name"],
			Alias:       o.props["port.alias"],
			AliasPrefix: aliasPrefix(o.props["port.alias"]),
			Path:        o.props["object.path"],
		}
	}
	return s
}

// teardown ends the connection's session: pending requests fail with the reason, and provenance and metrics records
// are dropped. the failures stay in the graph's resolved history so the last snapshot shows them.
func (g *Graph) teardown(reason string) {
	for _, r := range g.pendingByID() {
		g.resolve(r, false, reason)
	}
	g.session = newSessionState()
	g.summary = MetricsSummary{} // no metrics state is shown as current once the connection is gone
	g.dirty = true
}

func flattenEntries(entries map[uint32]map[string]MetadataEntry) []MetadataEntry {
	var out []MetadataEntry
	for _, subject := range entries {
		for _, e := range subject {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func propUint32(props map[string]string, key string) uint32 {
	v, err := strconv.ParseUint(props[key], 10, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}
