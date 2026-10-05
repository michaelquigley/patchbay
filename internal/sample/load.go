// Package sample loads captured pw-dump.json files into the same snapshot type the live backend produces.
package sample

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/pkg/errors"
)

// DumpFile is the name of the pw-dump capture inside a sample directory.
const DumpFile = "pw-dump.json"

// Load reads dir/pw-dump.json into a live snapshot.
func Load(dir string) (*pipewire.Snapshot, error) {
	return LoadFile(filepath.Join(dir, DumpFile))
}

// LoadFile reads one pw-dump.json into a live snapshot.
func LoadFile(path string) (*pipewire.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "reading '%v'", path)
	}
	inputs, err := Inputs(data)
	if err != nil {
		return nil, errors.Wrapf(err, "decoding '%v'", path)
	}
	return pipewire.Replay(inputs), nil
}

// dumped is one pw-dump entry, read out of its generic map. pw-dump is a loosely typed third-party document, so it
// has no schema here: the loader picks out the few fields it needs and carries everything else as properties.
type dumped struct {
	id        uint32
	typ       string
	version   uint32
	props     map[string]string
	info      map[string]any
	metadata  []any
	serial    pipewire.Serial
	hasSerial bool
}

// Inputs translates a pw-dump capture into the inputs a live enumeration would deliver: every global first, then
// each bound object's info and metadata properties, as the registry and the binds deliver them on a live connection.
// pw-dump writes objects in its own enumeration order, which is close to but not exactly registration order; globals
// are announced in object.serial order instead, which is the order the daemon registered them in and so the order a
// live registry delivers them. references are resolved only at announcement, so this order is load-bearing.
func Inputs(data []byte) ([]pipewire.Input, error) {
	// dd binds only documents with an object at the root; pw-dump's root is an array, so it is decoded into bare
	// values and walked by hand. UseNumber keeps numeric properties in their literal form.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root []any
	if err := dec.Decode(&root); err != nil {
		return nil, errors.Wrap(err, "pw-dump root is not an array of objects")
	}

	objects := make([]dumped, 0, len(root))
	for i, v := range root {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, errors.Errorf("pw-dump entry %d is %T, not an object", i, v)
		}
		o := dumped{
			id:       uint32(number(m["id"])),
			typ:      str(m["type"]),
			version:  uint32(number(m["version"])),
			info:     object(m["info"]),
			metadata: array(m["metadata"]),
		}
		props := object(m["props"])
		if p := object(o.info["props"]); p != nil {
			props = p
		}
		o.props = stringProps(props)
		if serial, err := strconv.ParseUint(o.props["object.serial"], 10, 64); err == nil {
			o.serial, o.hasSerial = pipewire.Serial(serial), true
		} else {
			dl.Warnf("%v %d has no usable object.serial; announced last", o.typ, o.id)
		}
		objects = append(objects, o)
	}
	sort.SliceStable(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		if !a.hasSerial || !b.hasSerial {
			return a.hasSerial && !b.hasSerial
		}
		return a.serial < b.serial
	})

	var globals, infos []pipewire.Input
	for _, o := range objects {
		globals = append(globals, pipewire.GlobalAdded{ID: o.id, Type: o.typ, Version: o.version, Props: o.props})
		if o.info == nil && o.metadata == nil {
			continue
		}
		state, errmsg := str(o.info["state"]), str(o.info["error"])
		switch o.typ {
		case "PipeWire:Interface:Node":
			infos = append(infos, pipewire.NodeInfo{Serial: o.serial, State: state, Error: errmsg, Props: o.props})
		case "PipeWire:Interface:Port":
			direction := pipewire.DirectionOut
			if str(o.info["direction"]) == "input" {
				direction = pipewire.DirectionIn
			}
			infos = append(infos, pipewire.PortInfo{Serial: o.serial, Direction: direction, Props: o.props})
		case "PipeWire:Interface:Link":
			infos = append(infos, pipewire.LinkInfo{Serial: o.serial, State: state, Error: errmsg, Props: o.props})
		case "PipeWire:Interface:Device", "PipeWire:Interface:Client":
			infos = append(infos, pipewire.ObjectInfo{Serial: o.serial, Props: o.props})
		case "PipeWire:Interface:Metadata":
			for _, v := range o.metadata {
				e := object(v)
				infos = append(infos, pipewire.MetadataProperty{
					Serial:  o.serial,
					Subject: uint32(number(e["subject"])),
					Key:     str(e["key"]),
					Type:    str(e["type"]),
					Value:   render(e["value"]),
				})
			}
		}
	}
	return append(globals, infos...), nil
}

// stringProps renders dump props the way a live spa_dict carries them: strings verbatim, everything else as its json
// text (numbers and booleans come out as "48000" and "true").
func stringProps(props map[string]any) map[string]string {
	out := make(map[string]string, len(props))
	for k, v := range props {
		out[k] = render(v)
	}
	return out
}

func render(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	default:
		text, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(text)
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func number(v any) int64 {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}
	i, err := n.Int64()
	if err != nil {
		return 0
	}
	return i
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func array(v any) []any {
	a, _ := v.([]any)
	return a
}
