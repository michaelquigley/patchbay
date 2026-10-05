#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "pipewire.h"
#include "_cgo_export.h"

struct pb_conn {
	uintptr_t handle;
	struct pw_thread_loop *loop;
	struct pw_context *context;
	struct pw_core *core;
	struct pw_registry *registry;
	struct spa_hook core_listener;
	struct spa_hook registry_listener;
	struct spa_source *wake;
	struct spa_list objects;
};

struct pb_obj {
	struct spa_list link;
	pb_conn *conn;
	struct pw_proxy *proxy;
	struct spa_hook object_listener;
	struct spa_hook proxy_listener;
	int kind;
	uint64_t serial;
};

// every callback that hands an observation to go signals the wake event, so the go side folds the batch into one
// snapshot after the loop finishes the current dispatch.
static void wake(pb_conn *c) {
	pw_loop_signal_event(pw_thread_loop_get_loop(c->loop), c->wake);
}

static void on_wake(void *data, uint64_t count) {
	pb_conn *c = data;
	pbWake(c->handle);
}

// core

static void on_core_done(void *data, uint32_t id, int seq) {
	pb_conn *c = data;
	pbCoreDone(c->handle, id, seq);
	wake(c);
}

static void on_core_error(void *data, uint32_t id, int seq, int res, const char *message) {
	pb_conn *c = data;
	pbCoreError(c->handle, id, seq, res, (char *) (message ? message : ""));
	wake(c);
}

static const struct pw_core_events core_events = {
	PW_VERSION_CORE_EVENTS,
	.done = on_core_done,
	.error = on_core_error,
};

// registry

static void on_global(void *data, uint32_t id, uint32_t permissions, const char *type, uint32_t version,
		const struct spa_dict *props) {
	pb_conn *c = data;
	pbGlobal(c->handle, id, (char *) type, version, (struct spa_dict *) props);
	wake(c);
}

static void on_global_remove(void *data, uint32_t id) {
	pb_conn *c = data;
	pbGlobalRemove(c->handle, id);
	wake(c);
}

static const struct pw_registry_events registry_events = {
	PW_VERSION_REGISTRY_EVENTS,
	.global = on_global,
	.global_remove = on_global_remove,
};

// bound objects

static void on_node_info(void *data, const struct pw_node_info *info) {
	pb_obj *o = data;
	int changed = (info->change_mask & PW_NODE_CHANGE_MASK_PROPS) != 0;
	pbNodeInfo(o->conn->handle, o->serial, (char *) pw_node_state_as_string(info->state),
			(char *) (info->error ? info->error : ""), changed, (struct spa_dict *) info->props);
	wake(o->conn);
}

static const struct pw_node_events node_events = {
	PW_VERSION_NODE_EVENTS,
	.info = on_node_info,
};

static void on_port_info(void *data, const struct pw_port_info *info) {
	pb_obj *o = data;
	int changed = (info->change_mask & PW_PORT_CHANGE_MASK_PROPS) != 0;
	int input = info->direction == SPA_DIRECTION_INPUT;
	pbPortInfo(o->conn->handle, o->serial, input, changed, (struct spa_dict *) info->props);
	wake(o->conn);
}

static const struct pw_port_events port_events = {
	PW_VERSION_PORT_EVENTS,
	.info = on_port_info,
};

static void on_link_info(void *data, const struct pw_link_info *info) {
	pb_obj *o = data;
	int changed = (info->change_mask & PW_LINK_CHANGE_MASK_PROPS) != 0;
	pbLinkInfo(o->conn->handle, o->serial, (char *) pw_link_state_as_string(info->state),
			(char *) (info->error ? info->error : ""), changed, (struct spa_dict *) info->props);
	wake(o->conn);
}

static const struct pw_link_events link_events = {
	PW_VERSION_LINK_EVENTS,
	.info = on_link_info,
};

static void on_device_info(void *data, const struct pw_device_info *info) {
	pb_obj *o = data;
	int changed = (info->change_mask & PW_DEVICE_CHANGE_MASK_PROPS) != 0;
	pbObjectInfo(o->conn->handle, o->serial, changed, (struct spa_dict *) info->props);
	wake(o->conn);
}

static const struct pw_device_events device_events = {
	PW_VERSION_DEVICE_EVENTS,
	.info = on_device_info,
};

static void on_client_info(void *data, const struct pw_client_info *info) {
	pb_obj *o = data;
	int changed = (info->change_mask & PW_CLIENT_CHANGE_MASK_PROPS) != 0;
	pbObjectInfo(o->conn->handle, o->serial, changed, (struct spa_dict *) info->props);
	wake(o->conn);
}

static const struct pw_client_events client_events = {
	PW_VERSION_CLIENT_EVENTS,
	.info = on_client_info,
};

static int on_metadata_property(void *data, uint32_t subject, const char *key, const char *type, const char *value) {
	pb_obj *o = data;
	pbMetadataProperty(o->conn->handle, o->serial, subject, (char *) key, (char *) type, (char *) value);
	wake(o->conn);
	return 0;
}

static const struct pw_metadata_events metadata_events = {
	PW_VERSION_METADATA_EVENTS,
	.property = on_metadata_property,
};

static void on_proxy_error(void *data, int seq, int res, const char *message) {
	pb_obj *o = data;
	pbProxyError(o->conn->handle, o->serial, res, (char *) (message ? message : ""));
	wake(o->conn);
}

static const struct pw_proxy_events proxy_events = {
	PW_VERSION_PROXY_EVENTS,
	.error = on_proxy_error,
};

// lifecycle

void pb_init(void) {
	pw_init(NULL, NULL);
}

pb_conn *pb_conn_new(uintptr_t handle, char *err, size_t errlen) {
	pb_conn *c = calloc(1, sizeof(*c));
	if (c == NULL) {
		snprintf(err, errlen, "out of memory");
		return NULL;
	}
	c->handle = handle;
	spa_list_init(&c->objects);

	c->loop = pw_thread_loop_new("patchbay", NULL);
	if (c->loop == NULL) {
		snprintf(err, errlen, "cannot create thread loop: %s", strerror(errno));
		free(c);
		return NULL;
	}
	c->context = pw_context_new(pw_thread_loop_get_loop(c->loop), NULL, 0);
	if (c->context == NULL) {
		snprintf(err, errlen, "cannot create context: %s", strerror(errno));
		pw_thread_loop_destroy(c->loop);
		free(c);
		return NULL;
	}
	if (pw_thread_loop_start(c->loop) < 0) {
		snprintf(err, errlen, "cannot start thread loop: %s", strerror(errno));
		pw_context_destroy(c->context);
		pw_thread_loop_destroy(c->loop);
		free(c);
		return NULL;
	}

	pw_thread_loop_lock(c->loop);
	c->core = pw_context_connect(c->context, NULL, 0);
	if (c->core == NULL) {
		snprintf(err, errlen, "cannot connect to pipewire: %s", strerror(errno));
		pw_thread_loop_unlock(c->loop);
		pw_thread_loop_stop(c->loop);
		pw_context_destroy(c->context);
		pw_thread_loop_destroy(c->loop);
		free(c);
		return NULL;
	}
	pw_core_add_listener(c->core, &c->core_listener, &core_events, c);
	c->wake = pw_loop_add_event(pw_thread_loop_get_loop(c->loop), on_wake, c);
	return c;
}

int pb_conn_attach(pb_conn *c) {
	c->registry = pw_core_get_registry(c->core, PW_VERSION_REGISTRY, 0);
	if (c->registry == NULL) {
		return -errno;
	}
	pw_registry_add_listener(c->registry, &c->registry_listener, &registry_events, c);
	return 0;
}

void pb_conn_lock(pb_conn *c) {
	pw_thread_loop_lock(c->loop);
}

void pb_conn_unlock(pb_conn *c) {
	pw_thread_loop_unlock(c->loop);
}

static void obj_destroy(pb_obj *o) {
	spa_hook_remove(&o->object_listener);
	spa_hook_remove(&o->proxy_listener);
	spa_list_remove(&o->link);
	pw_proxy_destroy(o->proxy);
	free(o);
}

void pb_conn_free(pb_conn *c) {
	pb_obj *o;

	pw_thread_loop_lock(c->loop);
	spa_list_consume(o, &c->objects, link) {
		obj_destroy(o);
	}
	if (c->registry != NULL) {
		spa_hook_remove(&c->registry_listener);
		pw_proxy_destroy((struct pw_proxy *) c->registry);
	}
	if (c->wake != NULL) {
		pw_loop_destroy_source(pw_thread_loop_get_loop(c->loop), c->wake);
	}
	spa_hook_remove(&c->core_listener);
	pw_core_disconnect(c->core);
	pw_thread_loop_unlock(c->loop);

	pw_thread_loop_stop(c->loop);
	pw_context_destroy(c->context);
	pw_thread_loop_destroy(c->loop);
	free(c);
}

// binding

pb_obj *pb_bind(pb_conn *c, uint32_t id, const char *type, uint32_t version, int kind, uint64_t serial) {
	uint32_t ours;
	switch (kind) {
	case PB_NODE: ours = PW_VERSION_NODE; break;
	case PB_PORT: ours = PW_VERSION_PORT; break;
	case PB_LINK: ours = PW_VERSION_LINK; break;
	case PB_DEVICE: ours = PW_VERSION_DEVICE; break;
	case PB_CLIENT: ours = PW_VERSION_CLIENT; break;
	case PB_METADATA: ours = PW_VERSION_METADATA; break;
	default: return NULL;
	}

	struct pw_proxy *proxy = pw_registry_bind(c->registry, id, type, SPA_MIN(version, ours), 0);
	if (proxy == NULL) {
		return NULL;
	}
	pb_obj *o = calloc(1, sizeof(*o));
	if (o == NULL) {
		pw_proxy_destroy(proxy);
		return NULL;
	}
	o->conn = c;
	o->proxy = proxy;
	o->kind = kind;
	o->serial = serial;

	switch (kind) {
	case PB_NODE:
		pw_node_add_listener((struct pw_node *) proxy, &o->object_listener, &node_events, o);
		break;
	case PB_PORT:
		pw_port_add_listener((struct pw_port *) proxy, &o->object_listener, &port_events, o);
		break;
	case PB_LINK:
		pw_link_add_listener((struct pw_link *) proxy, &o->object_listener, &link_events, o);
		break;
	case PB_DEVICE:
		pw_device_add_listener((struct pw_device *) proxy, &o->object_listener, &device_events, o);
		break;
	case PB_CLIENT:
		pw_client_add_listener((struct pw_client *) proxy, &o->object_listener, &client_events, o);
		break;
	case PB_METADATA:
		pw_metadata_add_listener((struct pw_metadata *) proxy, &o->object_listener, &metadata_events, o);
		break;
	}
	pw_proxy_add_listener(proxy, &o->proxy_listener, &proxy_events, o);
	spa_list_append(&c->objects, &o->link);
	return o;
}

void pb_unbind(pb_obj *o) {
	obj_destroy(o);
}

int pb_sync(pb_conn *c) {
	return pw_core_sync(c->core, PW_ID_CORE, 0);
}

// dictionaries

uint32_t pb_dict_n(const struct spa_dict *d) {
	return d == NULL ? 0 : d->n_items;
}

const char *pb_dict_key(const struct spa_dict *d, uint32_t i) {
	return d->items[i].key;
}

const char *pb_dict_value(const struct spa_dict *d, uint32_t i) {
	return d->items[i].value;
}
