package pipewire

import (
	"fmt"
	"strings"
)

// Serial is a pipewire object.serial: the only handle that is unique across the lifetime of one daemon. protocol ids
// recycle within seconds and are carried for requests only.
type Serial uint64

// ConnState is the backend's connection state as published in every snapshot.
type ConnState int

const (
	// Connecting covers the time between opening a connection and the end of initial observation (the sync barrier).
	Connecting ConnState = iota
	// Live means initial observation is complete and the snapshot tracks the daemon.
	Live
	// Disconnected means the daemon is unreachable; the snapshot's graph is the last one seen and is not current.
	Disconnected
)

func (s ConnState) String() string {
	switch s {
	case Connecting:
		return "connecting"
	case Live:
		return "live"
	case Disconnected:
		return "disconnected"
	default:
		return fmt.Sprintf("connState(%d)", int(s))
	}
}

// Direction is a port's direction.
type Direction int

const (
	DirectionUnknown Direction = iota
	DirectionIn
	DirectionOut
)

func (d Direction) String() string {
	switch d {
	case DirectionIn:
		return "in"
	case DirectionOut:
		return "out"
	default:
		return "unknown"
	}
}

// Media is a port's media kind.
type Media int

const (
	MediaUnknown Media = iota
	MediaAudio
	MediaMIDI
	MediaVideo
)

func (m Media) String() string {
	switch m {
	case MediaAudio:
		return "audio"
	case MediaMIDI:
		return "midi"
	case MediaVideo:
		return "video"
	default:
		return "unknown"
	}
}

// Node is an observed pipewire node.
type Node struct {
	Serial       Serial
	ID           uint32
	Props        map[string]string
	Name         string // node.name
	AppName      string // application.name
	Description  string // node.description
	Nick         string // node.nick
	MediaClass   string // media.class; empty for jack clients such as REAPER
	HasDevice    bool   // device.id is present, whether or not it resolved
	DeviceSerial Serial // the device named by device.id, resolved at announcement; zero when none or unresolved
	State        string // bound info state: error, creating, suspended, idle, running
	Error        string
}

// Port is an observed pipewire port.
type Port struct {
	Serial      Serial
	ID          uint32
	Props       map[string]string
	NodeSerial  Serial // zero when the owning node is not (yet) observed
	NodeID      uint32
	Direction   Direction
	Media       Media
	Monitor     bool   // port.monitor
	Name        string // port.name
	Alias       string // port.alias
	AliasPrefix string // the text of port.alias before its first colon; empty when there is no colon
	Path        string // object.path: for an alsa port, its pcm and channel (alsa:pcm:1:hw:1:capture:capture_3)
}

// Link is an observed pipewire link. its endpoints are resolved to serials at the time the link appeared, since the
// ids it names can be reused by other ports later.
type Link struct {
	Serial  Serial
	ID      uint32
	Props   map[string]string
	OutPort Serial
	InPort  Serial
	OutNode Serial
	InNode  Serial
	State   string // bound info state: error, unlinked, init, negotiating, allocating, paused, active
	Error   string
	// CreatedHere is true only for the one link lifetime a request of this process was confirmed on: the link global
	// with the request's bound id, its endpoints the requested serials, active or paused. every other link, however it
	// matches a request, is observed.
	CreatedHere bool
}

// Device is an observed pipewire device.
type Device struct {
	Serial Serial
	ID     uint32
	Props  map[string]string
	// HardwareSerial is device.serial: the hardware's own serial, which survives a replug and a profile change where
	// the object serial does not. empty when the device reports none (built-in cards report none).
	HardwareSerial string
	// ALSACard is api.alsa.card, the kernel's card number for an alsa device, valid only when HasALSACard is set.
	ALSACard    int
	HasALSACard bool
}

// Client is an observed pipewire client.
type Client struct {
	Serial Serial
	ID     uint32
	Props  map[string]string
}

// Settings is the decoded subject-0 content of the settings metadata. zero means absent or released.
type Settings struct {
	Present      bool
	ForceSeen    bool // the settings metadata shows clock.force-quantum; without it ForceQuantum is unknown, not 0
	Rate         int
	Quantum      int
	MinQuantum   int
	MaxQuantum   int
	ForceQuantum int
	ForceRate    int
}

// MetadataEntry is one property of a metadata object.
type MetadataEntry struct {
	Subject uint32 // the protocol id the entry names; 0 is global
	// SubjectSerial is the object the subject id named, resolved when the entry arrived (or, for entries delivered
	// during initial enumeration, when the barrier latched); zero when unresolved, when the subject is global, or
	// once that object has been removed. it is never re-resolved, so a reused id never inherits a stale entry.
	SubjectSerial Serial
	Key           string
	Type          string
	Value         string
}

// Snapshot is an immutable view of the observed graph. a published snapshot is never modified; the pointer changes
// only when content does.
type Snapshot struct {
	Generation uint64
	// Session numbers the connection the graph was observed on. it increases with every new connection, so a consumer
	// that missed every snapshot between a disconnect and the return to live still sees that the graph is a new one:
	// serials can repeat across a daemon restart, sessions do not.
	Session  uint64
	State    ConnState
	Error    string // the reason for the most recent disconnect, while disconnected
	Nodes    map[Serial]Node
	Ports    map[Serial]Port
	Links    map[Serial]Link
	Devices  map[Serial]Device
	Clients  map[Serial]Client
	Settings Settings
	Default  []MetadataEntry // the default metadata, read-only, for the inspector's policy view
	Requests []Request       // pending requests, then recent resolved ones; the reliable record of outcomes
	Metrics  MetricsSummary  // the profiler's summary, published at most ten times a second

	// Unresolved counts references that named an object not observed when they were announced: ports with no owner,
	// links with a missing endpoint, and nodes whose device.id names no observed device. they are shown, not guessed
	// at.
	Unresolved int
}

func emptySnapshot(state ConnState) *Snapshot {
	return &Snapshot{
		State:   state,
		Nodes:   map[Serial]Node{},
		Ports:   map[Serial]Port{},
		Links:   map[Serial]Link{},
		Devices: map[Serial]Device{},
		Clients: map[Serial]Client{},
	}
}

// mediaOf classifies a port from its format.dsp, falling back to the owning node's media.class for ports (video)
// that carry no dsp format.
func mediaOf(formatDSP, nodeMediaClass string) Media {
	f := strings.ToLower(formatDSP)
	switch {
	case strings.Contains(f, "midi"), strings.Contains(f, "ump"):
		return MediaMIDI
	case strings.Contains(f, "audio"):
		return MediaAudio
	case strings.Contains(nodeMediaClass, "Video"):
		return MediaVideo
	}
	return MediaUnknown
}

// aliasPrefix is the text of a port alias before its first colon, or empty when there is none.
func aliasPrefix(alias string) string {
	prefix, _, found := strings.Cut(alias, ":")
	if !found {
		return ""
	}
	return prefix
}

func directionOf(v string) Direction {
	switch v {
	case "in", "input":
		return DirectionIn
	case "out", "output":
		return DirectionOut
	}
	return DirectionUnknown
}
