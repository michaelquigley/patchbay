// Package model is Patchbay's presentation model: it derives blocks from an observed snapshot, recognizes them
// against the remembered workspace, and produces the view the canvas draws. it never changes routing or settings.
package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/workspace"
)

// Key is a recognition key: (class, media, direction).
type Key = workspace.Key

// media and direction names as they appear in recognition keys.
const (
	MediaAudio = "audio"
	MediaMIDI  = "midi"
	MediaVideo = "video"

	DirectionIn  = "in"
	DirectionOut = "out"
)

// class prefixes of a recognition key.
const (
	classNode = "node:" // device-backed nodes, by node.name
	classApp  = "app:"  // client nodes, by application.name or node.name
	classMIDI = "midi:" // Midi-Bridge ports, by the alias prefix (the alsa client name)
)

const midiBridgeClass = "Midi/Bridge"

// Owner is the kind of thing a block presents.
type Owner int

const (
	OwnerDevice Owner = iota // a device-backed node
	OwnerClient              // a client node: an application
	OwnerBridge              // an alsa client on the Midi-Bridge
)

// BlockID names one live block instance. it is built from the owning node's serial, so it is valid for that node's
// lifetime within one connection and is never stored.
type BlockID string

// identity is how one port is presented: the block instance it belongs to, the block's recognition key, and the port's
// key within the block.
type identity struct {
	block   BlockID
	node    pipewire.Serial
	key     Key
	owner   Owner
	keyed   bool // false when the identifying property is empty; such a block is never matched or recorded
	portKey string
	label   string
	title   string
	// hardware is the device's own serial (device.serial) for a device-backed block: the association chooser's hint,
	// never a recognition key.
	hardware string
}

// identify derives a port's identity, or reports false for a port that belongs to no block (no observed owner, no
// known media or direction).
func identify(snap *pipewire.Snapshot, port pipewire.Port) (identity, bool) {
	node, ok := snap.Nodes[port.NodeSerial]
	if !ok {
		return identity{}, false
	}
	media := mediaName(port.Media)
	direction := directionName(port.Direction)
	if media == "" || direction == "" {
		return identity{}, false
	}

	var id identity
	var name, prefix string
	switch {
	case node.MediaClass == midiBridgeClass:
		// hardware midi arrives as ports on one bridge node; the alias prefix names the alsa client, the owner.
		prefix = port.AliasPrefix
		name = prefix
		id.owner = OwnerBridge
		id.key.Class = classMIDI + prefix
		id.portKey = port.Alias
		id.label = labelAfterPrefix(port)
		id.title = prefix
	case node.HasDevice:
		// a device-backed node stays one even when its device did not resolve; the backend counts that miss.
		name = deviceNodeName(node.Name)
		id.owner = OwnerDevice
		id.key.Class = classNode + name
		id.hardware = snap.Devices[node.DeviceSerial].HardwareSerial
		id.portKey = port.Name
		id.label = port.Name
		id.title = firstOf(node.Description, node.Nick, node.Name)
	default:
		name = firstOf(node.AppName, node.Name)
		id.owner = OwnerClient
		id.key.Class = classApp + name
		id.portKey = port.Alias
		id.label = labelAfterPrefix(port)
		id.title = name
	}
	id.node = node.Serial
	id.key.Media = media
	id.key.Direction = direction
	id.keyed = name != ""
	id.block = BlockID(fmt.Sprintf("%d|%s|%s|%s", node.Serial, prefix, media, direction))
	if id.label == "" {
		id.label = fmt.Sprintf("port %d", port.Serial)
	}
	if id.title == "" {
		id.title = fmt.Sprintf("node %d", node.Serial)
	}
	return id, true
}

// deviceNodeName is a device-backed node's name without the counter WirePlumber appends when the name is already
// taken (alsa_input.pci-0000_0d_00.4.analog-stereo.3). the counter starts at 2 and depends on what was created
// before, so it is a runtime value; on seven it differed between captures of the same devices. two live nodes that
// differ only by it share a key and are presented as new, as any collision is.
func deviceNodeName(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil || n < 2 || n > 99 || strconv.Itoa(n) != name[i+1:] {
		return name
	}
	return name[:i]
}

// categoryClass computes a port's category class. classes are computed, never stored, so a port follows its class.
func categoryClass(snap *pipewire.Snapshot, port pipewire.Port) string {
	if node, ok := snap.Nodes[port.NodeSerial]; ok && strings.Contains(node.MediaClass, "Video") {
		return workspace.ClassVideo
	}
	if port.Monitor {
		return workspace.ClassMonitor
	}
	return ""
}

func mediaName(m pipewire.Media) string {
	switch m {
	case pipewire.MediaAudio:
		return MediaAudio
	case pipewire.MediaMIDI:
		return MediaMIDI
	case pipewire.MediaVideo:
		return MediaVideo
	}
	return ""
}

func directionName(d pipewire.Direction) string {
	switch d {
	case pipewire.DirectionIn:
		return DirectionIn
	case pipewire.DirectionOut:
		return DirectionOut
	}
	return ""
}

// labelAfterPrefix is a port's alias without its owner prefix.
func labelAfterPrefix(port pipewire.Port) string {
	if port.AliasPrefix == "" {
		return port.Alias
	}
	return strings.TrimPrefix(port.Alias, port.AliasPrefix+":")
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
