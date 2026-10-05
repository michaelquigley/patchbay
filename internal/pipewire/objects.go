package pipewire

import (
	"sort"
	"strconv"

	"github.com/michaelquigley/df/dl"
)

const (
	typeNode     = "PipeWire:Interface:Node"
	typePort     = "PipeWire:Interface:Port"
	typeLink     = "PipeWire:Interface:Link"
	typeDevice   = "PipeWire:Interface:Device"
	typeClient   = "PipeWire:Interface:Client"
	typeMetadata = "PipeWire:Interface:Metadata"

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

	// port; node is zero when the owner was not observed at the port's announcement
	direction Direction
	nodeID    uint32
	node      Serial

	// link; an endpoint is zero when it was not observed at the link's announcement
	outPort, inPort Serial
	outNode, inNode Serial

	// metadata
	metadataName string
	entries      map[uint32]map[string]MetadataEntry
}

// sessionState is everything the backend knows only for the lifetime of one connection. serials come from a
// per-daemon counter, so a restarted daemon can reuse numbers seen before; this state is therefore cleared explicitly
// on disconnect rather than trusted to miss on lookup.
type sessionState struct {
	pending     map[RequestID]*pendingRequest
	createdHere map[Serial]struct{}
	metrics     map[Serial]*metricsRecord
}

type pendingRequest struct {
	id RequestID
}

type metricsRecord struct {
	total    uint64
	baseline uint64
}

func newSessionState() sessionState {
	return sessionState{
		pending:     map[RequestID]*pendingRequest{},
		createdHere: map[Serial]struct{}{},
		metrics:     map[Serial]*metricsRecord{},
	}
}

// Graph folds inputs from one connection into snapshots. it is not safe for concurrent use; the native transport
// drives it from the loop thread only.
type Graph struct {
	drv     driver
	objects map[Serial]*object
	byID    map[uint32]Serial

	phase       barrierPhase
	syncSeq     int
	pendingInfo map[Serial]struct{}

	session sessionState

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
		dirty:       true,
	}
	return g
}

// start issues the first sync round trip. it must be called after the registry listener is attached.
func (g *Graph) start() {
	g.phase = awaitingFirstSync
	g.syncSeq = g.drv.sync()
}

// live reports whether initial observation is complete.
func (g *Graph) live() bool {
	return g.phase == barrierPassed && len(g.pendingInfo) == 0
}

// Apply folds one input into the graph's mutable state.
func (g *Graph) Apply(in Input) {
	wasLive := g.live()
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
	}
	g.dirty = true
	if !wasLive && g.live() {
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
	serial, err := strconv.ParseUint(in.Props["object.serial"], 10, 64)
	if err != nil || serial == 0 {
		dl.Warnf("ignoring %v %d without a usable object.serial (%q)", kind, in.ID, in.Props["object.serial"])
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

	o := &object{kind: kind, id: in.ID, serial: Serial(serial), props: in.Props}
	switch kind {
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
	}
	g.objects[o.serial] = o
	g.byID[o.id] = o.serial

	if g.drv.bind(in.ID, in.Type, in.Version, o.serial) {
		// metadata has no info event; its properties arrive on bind and are covered by the second sync.
		if kind != KindMetadata {
			g.pendingInfo[o.serial] = struct{}{}
		}
	} else {
		dl.Warnf("could not bind %v %d (serial %d)", kind, in.ID, o.serial)
	}
	if kind != KindMetadata {
		g.events = append(g.events, ObjectAppeared{Kind: kind, Serial: o.serial})
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
	if o.kind != KindMetadata {
		g.events = append(g.events, ObjectVanished{Kind: o.kind, Serial: serial})
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
		return
	}
	subject := o.entries[in.Subject]
	if in.Removed {
		delete(subject, in.Key)
		return
	}
	if subject == nil {
		subject = map[string]MetadataEntry{}
		o.entries[in.Subject] = subject
	}
	subject[in.Key] = MetadataEntry{Subject: in.Subject, Key: in.Key, Type: in.Type, Value: in.Value}
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
	for _, o := range g.objects {
		switch o.kind {
		case KindNode:
			s.Nodes[o.serial] = Node{
				Serial:     o.serial,
				ID:         o.id,
				Props:      o.props,
				Name:       o.props["node.name"],
				MediaClass: o.props["media.class"],
				State:      o.state,
				Error:      o.err,
			}
		case KindDevice:
			s.Devices[o.serial] = Device{Serial: o.serial, ID: o.id, Props: o.props}
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
		case KindMetadata:
			switch o.metadataName {
			case metadataSettings:
				s.Settings = decodeSettings(o.entries[0])
			case metadataDefault:
				s.Default = flattenEntries(o.entries)
			}
		}
	}
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
			Serial:     o.serial,
			ID:         o.id,
			Props:      o.props,
			NodeSerial: o.node,
			NodeID:     o.nodeID,
			Direction:  o.direction,
			Media:      mediaOf(o.props["format.dsp"], nodeClass),
			Monitor:    o.props["port.monitor"] == "true",
		}
	}
	return s
}

// teardown ends the connection's session: pending requests fail, and provenance and metrics records are dropped.
func (g *Graph) teardown(reason string) {
	ids := make([]RequestID, 0, len(g.session.pending))
	for id := range g.session.pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		g.events = append(g.events, RequestResolved{ID: id, OK: false, Reason: reason})
	}
	g.session = newSessionState()
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
