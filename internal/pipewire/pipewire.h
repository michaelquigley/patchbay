#ifndef PATCHBAY_PIPEWIRE_H
#define PATCHBAY_PIPEWIRE_H

// the native side of the backend: every libpipewire macro wrapper and listener table. the go side holds opaque
// pointers to these structs and exports only the callback trampolines; no go pointer is stored in c memory, the
// connection is identified to go by one cgo.Handle value.

#include <stdint.h>
#include <stddef.h>
#include <pipewire/pipewire.h>
#include <pipewire/extensions/metadata.h>

enum pb_kind {
	PB_NODE = 0,
	PB_PORT = 1,
	PB_LINK = 2,
	PB_DEVICE = 3,
	PB_CLIENT = 4,
	PB_METADATA = 5,
};

typedef struct pb_conn pb_conn;
typedef struct pb_obj pb_obj;

void pb_init(void);

// pb_conn_new starts a thread loop, connects to the daemon, and attaches the core listener. on success it returns
// with the loop locked; the caller attaches the registry and unlocks. on failure it returns NULL, unlocked, with err
// filled.
pb_conn *pb_conn_new(uintptr_t handle, char *err, size_t errlen);
int pb_conn_attach(pb_conn *c);
void pb_conn_lock(pb_conn *c);
void pb_conn_unlock(pb_conn *c);
void pb_conn_free(pb_conn *c);

// the remaining calls run on the loop thread or with the loop locked.
pb_obj *pb_bind(pb_conn *c, uint32_t id, const char *type, uint32_t version, int kind, uint64_t serial);
void pb_unbind(pb_obj *o);
int pb_sync(pb_conn *c);

uint32_t pb_dict_n(const struct spa_dict *d);
const char *pb_dict_key(const struct spa_dict *d, uint32_t i);
const char *pb_dict_value(const struct spa_dict *d, uint32_t i);

#endif
