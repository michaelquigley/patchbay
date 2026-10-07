package pipewire

import (
	"strconv"
	"strings"
)

// decodeSettings reads the subject-0 entries of the settings metadata. values that are absent or unparseable decode
// as zero, which every consumer treats as unknown.
func decodeSettings(entries map[string]MetadataEntry) Settings {
	s := Settings{Present: true}
	s.Rate = settingInt(entries, "clock.rate")
	s.Quantum = settingInt(entries, "clock.quantum")
	s.MinQuantum = settingInt(entries, "clock.min-quantum")
	s.MaxQuantum = settingInt(entries, "clock.max-quantum")
	// the override counts as seen only when it parses as a whole number of zero or more; anything else is unknown,
	// never read as automatic.
	if e, ok := entries["clock.force-quantum"]; ok {
		if v, err := strconv.Atoi(strings.TrimSpace(e.Value)); err == nil && v >= 0 {
			s.ForceQuantum, s.ForceSeen = v, true
		}
	}
	s.ForceRate = settingInt(entries, "clock.force-rate")
	return s
}

func settingInt(entries map[string]MetadataEntry, key string) int {
	e, ok := entries[key]
	if !ok {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(e.Value))
	if err != nil {
		return 0
	}
	return v
}
