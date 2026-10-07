#ifndef PATCHBAY_PIPEWIRE_H
#define PATCHBAY_PIPEWIRE_H

// the native side of the backend: every libpipewire macro wrapper and listener table. the go side holds opaque
// pointers to these structs and exports only the callback trampolines; no go pointer is stored in c memory, the
// connection is identified to go by one cgo.Handle value.

#include <stdint.h>
#include <stddef.h>
#include <pipewire/pipewire.h>
#include <pipewire/extensions/metadata.h>
#include <pipewire/extensions/profiler.h>

enum pb_kind {
	PB_NODE = 0,
	PB_PORT = 1,
	PB_LINK = 2,
	PB_DEVICE = 3,
	PB_CLIENT = 4,
	PB_METADATA = 5,
	PB_PROFILER = 6,
};

// one profiler object (one driver's cycle), decoded. followers beyond PB_MAX_FOLLOWERS are dropped.
#define PB_MAX_FOLLOWERS 256

struct pb_prof_block {
	int32_t id;
	int32_t status;
	int32_t has_xruns;
	int32_t xruns;
};

struct pb_prof_point {
	int64_t arrival; // CLOCK_MONOTONIC when the pod was received
	int32_t has_info;
	int32_t info_xruns;
	int32_t has_clock;
	int64_t nsec;
	int64_t duration;
	uint32_t rate_num;
	uint32_t rate_denom;
	int32_t has_driver;
	struct pb_prof_block driver;
	int32_t n_followers;
	struct pb_prof_block followers[PB_MAX_FOLLOWERS];
};

typedef struct pb_conn pb_conn;
typedef struct pb_obj pb_obj;
typedef struct pb_link pb_link;

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

// pb_metadata_set sets a property on a bound metadata object; an empty type is sent as none.
int pb_metadata_set(pb_obj *o, uint32_t subject, const char *key, const char *type, const char *value);

// pb_monotonic_ns reads CLOCK_MONOTONIC, the clock profiler pods are stamped with.
int64_t pb_monotonic_ns(void);

// pb_create_link asks the link factory for a lingering link and listens on its proxy for bound, error, and removed,
// routed to go with token. pb_release_link drops the proxy; the lingering link outlives it.
pb_link *pb_create_link(pb_conn *c, uint32_t out_node, uint32_t out_port, uint32_t in_node, uint32_t in_port,
		uint64_t token);
void pb_release_link(pb_link *l);
int pb_destroy_global(pb_conn *c, uint32_t id);

// pb_create_test_sink creates a non-lingering null audio sink owned by this connection, for live tests that must
// patch only objects they create; pb_destroy_test_sink removes it. nothing in the application calls them. both run
// with the loop locked.
struct pw_proxy *pb_create_test_sink(pb_conn *c, const char *name);
void pb_destroy_test_sink(struct pw_proxy *p);

// pb_conn_wake signals the loop's wake event. it is the one call that is safe from any thread.
void pb_conn_wake(pb_conn *c);

uint32_t pb_dict_n(const struct spa_dict *d);
const char *pb_dict_key(const struct spa_dict *d, uint32_t i);
const char *pb_dict_value(const struct spa_dict *d, uint32_t i);

#endif
