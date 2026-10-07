package model

import (
	"sort"
	"strconv"
	"unicode"

	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// derived is one block instance as the snapshot presents it, before recognition.
type derived struct {
	id       BlockID
	node     pipewire.Serial
	key      Key
	owner    Owner
	keyed    bool
	title    string
	hardware string // the device's own serial, for a device-backed block; empty otherwise
	ports    []derivedPort
	arrival  pipewire.Serial // lowest port serial; serials are monotonic, so this orders blocks by arrival
}

type derivedPort struct {
	serial pipewire.Serial
	key    string
	label  string
	class  string
}

// derive groups the snapshot's ports into block instances. ports with no observed owner or no known media or
// direction belong to no block.
func derive(snap *pipewire.Snapshot) map[BlockID]*derived {
	blocks := map[BlockID]*derived{}
	for _, port := range snap.Ports {
		id, ok := identify(snap, port)
		if !ok {
			continue
		}
		b := blocks[id.block]
		if b == nil {
			b = &derived{id: id.block, node: id.node, key: id.key, owner: id.owner, keyed: id.keyed, title: id.title,
				hardware: id.hardware, arrival: port.Serial}
			blocks[id.block] = b
		}
		if port.Serial < b.arrival {
			b.arrival = port.Serial
		}
		b.ports = append(b.ports, derivedPort{
			serial: port.Serial,
			key:    id.portKey,
			label:  id.label,
			class:  categoryClass(snap, port),
		})
	}
	for _, b := range blocks {
		sort.Slice(b.ports, func(i, j int) bool {
			if c := naturalCompare(b.ports[i].label, b.ports[j].label); c != 0 {
				return c < 0
			}
			return b.ports[i].serial < b.ports[j].serial
		})
	}
	return blocks
}

// naturalCompare orders labels with embedded numbers numerically, so in2 precedes in10.
func naturalCompare(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si := i
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na, _ := strconv.ParseUint(string(ra[si:i]), 10, 64)
			nb, _ := strconv.ParseUint(string(rb[sj:j]), 10, 64)
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if ra[i] != rb[j] {
			if ra[i] < rb[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(ra)-i < len(rb)-j:
		return -1
	case len(ra)-i > len(rb)-j:
		return 1
	}
	return 0
}
