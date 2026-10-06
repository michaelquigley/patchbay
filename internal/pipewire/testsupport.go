package pipewire

/*
#include <stdlib.h>
#include "pipewire.h"
*/
import "C"

import (
	"unsafe"

	"github.com/pkg/errors"
)

// invoke runs fn on the loop thread, with the loop lock held, through the same queue requests take, and waits for
// it. it returns "not connected" when no transport is current, and the disconnect reason when the connection is lost
// before fn runs. it is how the live tests reach libpipewire: every native call they make happens inside fn, on the
// loop thread, so none runs against a connection a loss has freed. nothing in the application calls it.
func (b *backend) invoke(fn func(driver)) error {
	b.mu.Lock()
	if b.current == nil {
		b.mu.Unlock()
		return errors.New("not connected")
	}
	done := make(chan error, 1)
	b.posted = append(b.posted, queued{invoke: fn, done: done})
	b.current.wake()
	b.mu.Unlock()
	return <-done
}

// createTestSink creates a null audio sink owned by the backend's current connection, for live tests that patch:
// they may create and destroy links only between sinks they create themselves. the sink does not linger, so it goes
// when remove is called or the connection closes. nothing in the application calls this.
func (b *backend) createTestSink(name string) (remove func(), err error) {
	var owner *nativeSession
	var sink *C.struct_pw_proxy
	err = b.invoke(func(d driver) {
		ns, ok := d.(*nativeSession)
		if !ok {
			return
		}
		cname := C.CString(name)
		defer C.free(unsafe.Pointer(cname))
		owner, sink = ns, C.pb_create_test_sink(ns.c, cname)
	})
	if err != nil {
		return nil, err
	}
	if sink == nil {
		return nil, errors.Errorf("could not create test sink '%v'", name)
	}
	return func() {
		// the sink belongs to the connection that made it: if another connection is current now, that one, and the
		// sink with it, are already gone. the comparison is by pointer only; the old session is never dereferenced.
		_ = b.invoke(func(d driver) {
			if ns, ok := d.(*nativeSession); ok && ns == owner {
				C.pb_destroy_test_sink(sink)
			}
		})
	}, nil
}
