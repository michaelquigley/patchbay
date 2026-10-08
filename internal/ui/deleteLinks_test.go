package ui

import (
	"maps"
	"testing"

	"github.com/michaelquigley/dfx"
)

func TestDeleteAfterLinkDisappearsDoesNothing(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	v := m.Reconcile(snap)
	c := &canvas{model: m, sel: newSelection()}
	c.track(v)
	fp := &fakePatcher{}
	a := &app{canvas: c, view: v, patching: newPatching(fp, m, func(s string) { t.Fatalf("unexpected notice: %s", s) })}
	a.deleteLinks()
	if len(fp.destroys) != 0 {
		t.Fatal("Delete without a selection posted a request")
	}
	link := v.Links[0].Serial
	c.sel.links[link] = true
	block := v.Blocks[0].ID
	c.sel.blocks[block] = true
	a.deleteLinks()
	if len(fp.destroys) != 1 || fp.destroys[0][1] != uint64(link) {
		t.Fatalf("first Delete = %v", fp.destroys)
	}
	c.track(v)
	if !c.sel.links[link] {
		t.Fatal("cleared selection before removal was observed")
	}
	// another selected link remains, until the operator explicitly clears the selection.
	kept := v.Links[1].Serial
	c.sel.links[kept] = true
	gone := *snap
	gone.Links = maps.Clone(snap.Links)
	delete(gone.Links, link)
	a.view = m.Reconcile(&gone)
	c.track(a.view)
	if c.sel.links[link] || !c.sel.links[kept] || !c.sel.blocks[block] {
		t.Fatalf("wrong selection after removal: %+v", c.sel)
	}
	c.apply(dfx.Intents[ID]{SelectionChanged: &dfx.SelectionChange[ID]{}}, frame{})
	a.deleteLinks()
	if len(fp.destroys) != 1 || len(a.patching.refused) != 0 {
		t.Fatalf("empty Delete posted a request or error: %v %v", fp.destroys, a.patching.refused)
	}
}
