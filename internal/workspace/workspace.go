// Package workspace is the one remembered workspace: positions, visibility, and associations of presentation blocks,
// persisted as yaml through dd. it holds presentation only; nothing here names a link, a quantum, or a routing choice.
package workspace

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/michaelquigley/df/dd"
	"github.com/michaelquigley/df/dl"
	"github.com/pkg/errors"
)

// Version is the workspace format version this build reads and writes.
const Version = 1

// Category class names, as stored in the category filter state.
const (
	ClassVideo   = "video"
	ClassMonitor = "monitor"
)

// Key is a recognition key: the stable identity a block is recognized by.
type Key struct {
	Class     string
	Media     string
	Direction string
}

// String renders the key as it appears in record keys.
func (k Key) String() string {
	return k.Class + "|" + k.Media + "|" + k.Direction
}

// Record is one remembered block, stored under its record key.
type Record struct {
	Key         Key
	X           float32
	Y           float32
	Hidden      bool     `dd:",+omitempty"`
	HiddenPorts []string `dd:",+omitempty"`
	Also        []Key    `dd:",+omitempty"` // other recognition keys the operator has associated with this record
	// Device is the hardware serial (device.serial) of the device the record's block was observed on, when it has
	// one. it orders the association chooser and names the profile case in the inspector; it is never matched on.
	Device string `dd:",+omitempty"`
}

// Matches reports whether the record answers to recognition key k, directly or through an association.
func (r *Record) Matches(k Key) bool {
	if r.Key == k {
		return true
	}
	for _, a := range r.Also {
		if a == k {
			return true
		}
	}
	return false
}

// PortHidden reports whether the port key is individually hidden.
func (r *Record) PortHidden(portKey string) bool {
	for _, k := range r.HiddenPorts {
		if k == portKey {
			return true
		}
	}
	return false
}

// SetPortHidden adds or removes a port key from the hidden set.
func (r *Record) SetPortHidden(portKey string, hidden bool) {
	out := r.HiddenPorts[:0:0]
	for _, k := range r.HiddenPorts {
		if k != portKey {
			out = append(out, k)
		}
	}
	if hidden {
		out = append(out, portKey)
	}
	r.HiddenPorts = out
}

// View is the canvas pan and zoom.
type View struct {
	PanX float32
	PanY float32
	Zoom float32
}

// Window is the last window size in logical screen coordinates.
type Window struct {
	Width  int
	Height int
}

// Panel is a side panel's expanded width and whether it is collapsed.
// a zero width uses the ui's default; collapsed defaults to false for older workspaces.
type Panel struct {
	Width     float32
	Collapsed bool
}

// Layout is the remembered window and side-panel presentation.
type Layout struct {
	Window      Window
	Inspector   Panel
	Performance Panel
}

// Workspace is the persisted record set. records are never deleted by the application.
type Workspace struct {
	Version       int
	View          View
	Layout        Layout
	HiddenClasses map[string]bool
	Records       map[string]*Record
}

// New returns a fresh workspace: video and monitor classes hidden, no records.
func New() *Workspace {
	return &Workspace{
		Version:       Version,
		View:          View{Zoom: 1},
		HiddenClasses: map[string]bool{ClassVideo: true, ClassMonitor: true},
		Records:       map[string]*Record{},
	}
}

// DefaultPath is ~/.config/patchbay/workspace.yaml.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", errors.Wrap(err, "locating the user config directory")
	}
	return filepath.Join(dir, "patchbay", "workspace.yaml"), nil
}

// Load reads the workspace at path; a missing file is a fresh workspace.
func Load(path string) (*Workspace, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	ws, err := dd.NewYAMLFile[Workspace](path)
	if err != nil {
		return nil, errors.Wrapf(err, "reading workspace '%v'", path)
	}
	if ws.Version != Version {
		return nil, errors.Errorf("workspace '%v' has version %d; this build reads version %d", path, ws.Version, Version)
	}
	if ws.HiddenClasses == nil {
		ws.HiddenClasses = map[string]bool{}
	}
	if ws.Records == nil {
		ws.Records = map[string]*Record{}
	}
	if ws.View.Zoom == 0 {
		ws.View.Zoom = 1
	}
	return ws, nil
}

// Save writes the workspace to path atomically.
func Save(ws *Workspace, path string) error {
	data, err := dd.UnbindYAML(ws)
	if err != nil {
		return errors.Wrap(err, "encoding workspace")
	}
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.Wrapf(err, "creating '%v'", filepath.Dir(path))
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".workspace-*.yaml")
	if err != nil {
		return errors.Wrapf(err, "creating a temporary file beside '%v'", path)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return errors.Wrapf(err, "writing '%v'", tmp.Name())
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return errors.Wrapf(err, "closing '%v'", tmp.Name())
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return errors.Wrapf(err, "replacing '%v'", path)
	}
	return nil
}

// Store saves a workspace with a short debounce. Changed encodes the workspace at once, on the caller's goroutine, so
// the caller may keep mutating it; the encoded bytes are written after the debounce, and Close writes any pending
// bytes immediately.
type Store struct {
	path     string
	debounce time.Duration

	mu      sync.Mutex
	pending []byte
	timer   *time.Timer
	closed  bool
}

// NewStore returns a store writing to path after debounce.
func NewStore(path string, debounce time.Duration) *Store {
	return &Store{path: path, debounce: debounce}
}

// Changed records that ws changed and schedules a save.
func (s *Store) Changed(ws *Workspace) {
	data, err := dd.UnbindYAML(ws)
	if err != nil {
		dl.Errorf("encoding workspace: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.pending = data
	if s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.flush)
	} else {
		s.timer.Reset(s.debounce)
	}
}

func (s *Store) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLocked()
}

func (s *Store) writeLocked() {
	if s.pending == nil {
		return
	}
	if err := writeFile(s.path, s.pending); err != nil {
		dl.Errorf("saving workspace: %v", err)
		return
	}
	s.pending = nil
}

// Close writes any pending change and stops the store.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.writeLocked()
	s.closed = true
}
