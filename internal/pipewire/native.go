package pipewire

/*
#cgo pkg-config: libpipewire-0.3
#include <stdlib.h>
#include "pipewire.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"syscall"
	"unsafe"

	"github.com/michaelquigley/df/dl"
	"github.com/pkg/errors"
)

var initOnce sync.Once

// nativeTransport reaches the user's daemon through libpipewire.
type nativeTransport struct{}

// nativeSession is one libpipewire connection. its driver methods are called only on the loop thread or with the
// loop locked.
type nativeSession struct {
	c    *C.pb_conn
	h    cgo.Handle
	sess *session
	objs map[Serial]*C.pb_obj
}

func (nativeTransport) open(sess *session) (transportSession, error) {
	initOnce.Do(func() { C.pb_init() })

	ns := &nativeSession{sess: sess, objs: map[Serial]*C.pb_obj{}}
	ns.h = cgo.NewHandle(ns)
	var errbuf [256]C.char
	c := C.pb_conn_new(C.uintptr_t(ns.h), &errbuf[0], C.size_t(len(errbuf)))
	if c == nil {
		ns.h.Delete()
		return nil, errors.New(C.GoString(&errbuf[0]))
	}
	// pb_conn_new returns with the loop locked, so no registry callback can arrive before begin has run.
	ns.c = c
	if rc := C.pb_conn_attach(c); rc < 0 {
		C.pb_conn_unlock(c)
		ns.close()
		return nil, errors.Errorf("cannot get registry: %v", syscall.Errno(-rc))
	}
	sess.begin(ns)
	C.pb_conn_unlock(c)
	return ns, nil
}

func (ns *nativeSession) close() {
	C.pb_conn_free(ns.c)
	ns.c = nil
	ns.objs = nil
	ns.h.Delete()
}

func kindCode(typ string) (C.int, bool) {
	k, ok := kindOf(typ)
	if !ok {
		return 0, false
	}
	switch k {
	case KindNode:
		return C.PB_NODE, true
	case KindPort:
		return C.PB_PORT, true
	case KindLink:
		return C.PB_LINK, true
	case KindDevice:
		return C.PB_DEVICE, true
	case KindClient:
		return C.PB_CLIENT, true
	case KindMetadata:
		return C.PB_METADATA, true
	}
	return 0, false
}

func (ns *nativeSession) bind(id uint32, typ string, version uint32, serial Serial) bool {
	code, ok := kindCode(typ)
	if !ok {
		return false
	}
	ctype := C.CString(typ)
	defer C.free(unsafe.Pointer(ctype))
	o := C.pb_bind(ns.c, C.uint32_t(id), ctype, C.uint32_t(version), code, C.uint64_t(serial))
	if o == nil {
		return false
	}
	ns.objs[serial] = o
	return true
}

func (ns *nativeSession) unbind(serial Serial) {
	if o, ok := ns.objs[serial]; ok {
		C.pb_unbind(o)
		delete(ns.objs, serial)
	}
}

func (ns *nativeSession) sync() int {
	return int(C.pb_sync(ns.c))
}

func sessionOf(h C.uintptr_t) *nativeSession {
	return cgo.Handle(h).Value().(*nativeSession)
}

func goDict(d *C.struct_spa_dict) map[string]string {
	n := C.pb_dict_n(d)
	props := make(map[string]string, int(n))
	for i := C.uint32_t(0); i < n; i++ {
		k := C.pb_dict_key(d, i)
		if k == nil {
			continue
		}
		v := C.pb_dict_value(d, i)
		if v == nil {
			props[C.GoString(k)] = ""
			continue
		}
		props[C.GoString(k)] = C.GoString(v)
	}
	return props
}

// changedDict converts a bound info's props only when the info says they changed; nil tells the graph to keep the
// props it has.
func changedDict(changed C.int, d *C.struct_spa_dict) map[string]string {
	if changed == 0 || d == nil {
		return nil
	}
	return goDict(d)
}

//export pbWake
func pbWake(h C.uintptr_t) {
	sessionOf(h).sess.flush()
}

//export pbCoreDone
func pbCoreDone(h C.uintptr_t, id C.uint32_t, seq C.int) {
	if id != C.PW_ID_CORE {
		return
	}
	sessionOf(h).sess.apply(SyncDone{Seq: int(seq)})
}

//export pbCoreError
func pbCoreError(h C.uintptr_t, id C.uint32_t, seq C.int, res C.int, message *C.char) {
	msg := C.GoString(message)
	if id == C.PW_ID_CORE {
		sessionOf(h).sess.lost(fmt.Sprintf("%s (%v)", msg, syscall.Errno(-res)))
		return
	}
	dl.Warnf("pipewire error on object %d: %s (%v)", uint32(id), msg, syscall.Errno(-res))
}

//export pbGlobal
func pbGlobal(h C.uintptr_t, id C.uint32_t, typ *C.char, version C.uint32_t, props *C.struct_spa_dict) {
	sessionOf(h).sess.apply(GlobalAdded{
		ID:      uint32(id),
		Type:    C.GoString(typ),
		Version: uint32(version),
		Props:   goDict(props),
	})
}

//export pbGlobalRemove
func pbGlobalRemove(h C.uintptr_t, id C.uint32_t) {
	sessionOf(h).sess.apply(GlobalRemoved{ID: uint32(id)})
}

//export pbNodeInfo
func pbNodeInfo(h C.uintptr_t, serial C.uint64_t, state *C.char, errmsg *C.char, changed C.int, props *C.struct_spa_dict) {
	sessionOf(h).sess.apply(NodeInfo{
		Serial: Serial(serial),
		State:  C.GoString(state),
		Error:  C.GoString(errmsg),
		Props:  changedDict(changed, props),
	})
}

//export pbPortInfo
func pbPortInfo(h C.uintptr_t, serial C.uint64_t, input C.int, changed C.int, props *C.struct_spa_dict) {
	direction := DirectionOut
	if input != 0 {
		direction = DirectionIn
	}
	sessionOf(h).sess.apply(PortInfo{
		Serial:    Serial(serial),
		Direction: direction,
		Props:     changedDict(changed, props),
	})
}

//export pbLinkInfo
func pbLinkInfo(h C.uintptr_t, serial C.uint64_t, state *C.char, errmsg *C.char, changed C.int, props *C.struct_spa_dict) {
	sessionOf(h).sess.apply(LinkInfo{
		Serial: Serial(serial),
		State:  C.GoString(state),
		Error:  C.GoString(errmsg),
		Props:  changedDict(changed, props),
	})
}

//export pbObjectInfo
func pbObjectInfo(h C.uintptr_t, serial C.uint64_t, changed C.int, props *C.struct_spa_dict) {
	sessionOf(h).sess.apply(ObjectInfo{Serial: Serial(serial), Props: changedDict(changed, props)})
}

//export pbMetadataProperty
func pbMetadataProperty(h C.uintptr_t, serial C.uint64_t, subject C.uint32_t, key, typ, value *C.char) {
	in := MetadataProperty{Serial: Serial(serial), Subject: uint32(subject)}
	if key != nil {
		in.Key = C.GoString(key)
	}
	if typ != nil {
		in.Type = C.GoString(typ)
	}
	if value == nil {
		in.Removed = true
	} else {
		in.Value = C.GoString(value)
	}
	sessionOf(h).sess.apply(in)
}

//export pbProxyError
func pbProxyError(h C.uintptr_t, serial C.uint64_t, res C.int, message *C.char) {
	sessionOf(h).sess.apply(ProxyError{
		Serial: Serial(serial),
		Error:  fmt.Sprintf("%s (%v)", C.GoString(message), syscall.Errno(-res)),
	})
}
