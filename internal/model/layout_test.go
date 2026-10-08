package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnchangedLayoutDoesNotSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m.SetLayout(m.Layout())
	m.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unchanged layout wrote a workspace: %v", err)
	}
}
