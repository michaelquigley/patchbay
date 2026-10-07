#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include <pipewire/impl-module.h>
#include <spa/pod/iter.h>
#include <spa/pod/parser.h>
#include <spa/param/profiler.h>

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
	struct spa_list links;
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

struct pb_link {
	struct spa_list link;
	pb_conn *conn;
	struct pw_proxy *proxy;
	struct spa_hook proxy_listener;
	uint64_t token;
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

// profiler: one pod per driver cycle, so decoding stays in c and go is called once per profiler object, with no
// wake: the go side aggregates and publishes summaries on its own clock.

static void parse_block(const struct spa_pod *pod, struct pb_prof_block *b) {
	char *name = NULL;
	int64_t prev_signal = 0, signal = 0, awake = 0, finish = 0;
	struct spa_fraction latency = { 0, 1 };
	int32_t xruns = -1;
	if (spa_pod_parse_struct(pod,
			SPA_POD_Int(&b->id),
			SPA_POD_String(&name),
			SPA_POD_Long(&prev_signal),
			SPA_POD_Long(&signal),
			SPA_POD_Long(&awake),
			SPA_POD_Long(&finish),
			SPA_POD_Int(&b->status),
			SPA_POD_Fraction(&latency),
			SPA_POD_OPT_Int(&xruns)) < 0) {
		b->id = -1;
		return;
	}
	b->has_xruns = xruns >= 0;
	b->xruns = b->has_xruns ? xruns : 0;
}

static void on_profile(void *data, const struct spa_pod *pod) {
	pb_obj *o = data;
	struct spa_pod *obj;
	int64_t arrival = pb_monotonic_ns();
	SPA_POD_STRUCT_FOREACH(pod, obj) {
		if (!spa_pod_is_object_type(obj, SPA_TYPE_OBJECT_Profiler)) {
			continue;
		}
		struct pb_prof_point point;
		memset(&point, 0, sizeof(point));
		point.arrival = arrival;
		struct spa_pod_prop *prop;
		SPA_POD_OBJECT_FOREACH((struct spa_pod_object *) obj, prop) {
			switch (prop->key) {
			case SPA_PROFILER_info: {
				int64_t counter = 0;
				float load0 = 0, load1 = 0, load2 = 0;
				int32_t xruns = 0;
				if (spa_pod_parse_struct(&prop->value,
						SPA_POD_Long(&counter),
						SPA_POD_Float(&load0),
						SPA_POD_Float(&load1),
						SPA_POD_Float(&load2),
						SPA_POD_Int(&xruns)) >= 0) {
					point.has_info = 1;
					point.info_xruns = xruns;
				}
				break;
			}
			case SPA_PROFILER_clock: {
				int32_t flags = 0, id = 0;
				char *name = NULL;
				struct spa_fraction rate = { 0, 1 };
				int64_t position = 0, delay = 0, next_nsec = 0;
				double rate_diff = 0;
				if (spa_pod_parse_struct(&prop->value,
						SPA_POD_Int(&flags),
						SPA_POD_Int(&id),
						SPA_POD_String(&name),
						SPA_POD_Long(&point.nsec),
						SPA_POD_Fraction(&rate),
						SPA_POD_Long(&position),
						SPA_POD_Long(&point.duration),
						SPA_POD_Long(&delay),
						SPA_POD_Double(&rate_diff),
						SPA_POD_Long(&next_nsec)) >= 0) {
					point.has_clock = 1;
					point.rate_num = rate.num;
					point.rate_denom = rate.denom;
				}
				break;
			}
			case SPA_PROFILER_driverBlock:
				parse_block(&prop->value, &point.driver);
				point.has_driver = point.driver.id >= 0;
				break;
			case SPA_PROFILER_followerBlock:
				if (point.n_followers < PB_MAX_FOLLOWERS) {
					parse_block(&prop->value, &point.followers[point.n_followers]);
					if (point.followers[point.n_followers].id >= 0) {
						point.n_followers++;
					}
				}
				break;
			}
		}
		pbProfile(o->conn->handle, &point);
	}
}

static const struct pw_profiler_events profiler_events = {
	PW_VERSION_PROFILER_EVENTS,
	.profile = on_profile,
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
	spa_list_init(&c->links);

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
	// the profiler interface's protocol support is not among a client context's default modules (metadata's is);
	// without it the profiler cannot be bound. failing to load it leaves monitoring unavailable, not the connection.
	if (pw_context_load_module(c->context, PW_EXTENSION_MODULE_PROFILER, NULL, NULL) == NULL) {
		pw_log_warn("patchbay: cannot load %s: %m", PW_EXTENSION_MODULE_PROFILER);
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

static void link_destroy(pb_link *l) {
	spa_hook_remove(&l->proxy_listener);
	spa_list_remove(&l->link);
	pw_proxy_destroy(l->proxy);
	free(l);
}

void pb_conn_free(pb_conn *c) {
	pb_obj *o;
	pb_link *l;

	pw_thread_loop_lock(c->loop);
	spa_list_consume(o, &c->objects, link) {
		obj_destroy(o);
	}
	spa_list_consume(l, &c->links, link) {
		link_destroy(l);
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
	case PB_PROFILER: ours = PW_VERSION_PROFILER; break;
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
	case PB_PROFILER:
		pw_profiler_add_listener((struct pw_profiler *) proxy, &o->object_listener, &profiler_events, o);
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

int pb_metadata_set(pb_obj *o, uint32_t subject, const char *key, const char *type, const char *value) {
	return pw_metadata_set_property((struct pw_metadata *) o->proxy, subject, key,
			(type != NULL && type[0] != '\0') ? type : NULL, value);
}

int64_t pb_monotonic_ns(void) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (int64_t) ts.tv_sec * 1000000000LL + ts.tv_nsec;
}

void pb_conn_wake(pb_conn *c) {
	wake(c);
}

// created links

// the link callbacks read everything they need from l before calling go: go never frees l during the call (releases
// are deferred to the wake handler), but nothing here relies on that.
static void on_link_bound(void *data, uint32_t global_id) {
	pb_link *l = data;
	pb_conn *c = l->conn;
	pbLinkBound(c->handle, l->token, global_id);
	wake(c);
}

static void on_link_removed(void *data) {
	pb_link *l = data;
	pb_conn *c = l->conn;
	pbLinkRemoved(c->handle, l->token);
	wake(c);
}

static void on_link_error(void *data, int seq, int res, const char *message) {
	pb_link *l = data;
	pb_conn *c = l->conn;
	pbLinkError(c->handle, l->token, res, (char *) (message ? message : ""));
	wake(c);
}

static const struct pw_proxy_events link_proxy_events = {
	PW_VERSION_PROXY_EVENTS,
	.bound = on_link_bound,
	.removed = on_link_removed,
	.error = on_link_error,
};

pb_link *pb_create_link(pb_conn *c, uint32_t out_node, uint32_t out_port, uint32_t in_node, uint32_t in_port,
		uint64_t token) {
	struct pw_properties *props = pw_properties_new(NULL, NULL);
	if (props == NULL) {
		return NULL;
	}
	pw_properties_setf(props, PW_KEY_LINK_OUTPUT_NODE, "%u", out_node);
	pw_properties_setf(props, PW_KEY_LINK_OUTPUT_PORT, "%u", out_port);
	pw_properties_setf(props, PW_KEY_LINK_INPUT_NODE, "%u", in_node);
	pw_properties_setf(props, PW_KEY_LINK_INPUT_PORT, "%u", in_port);
	pw_properties_set(props, PW_KEY_OBJECT_LINGER, "true");

	struct pw_proxy *proxy = pw_core_create_object(c->core, "link-factory", PW_TYPE_INTERFACE_Link,
			PW_VERSION_LINK, &props->dict, 0);
	pw_properties_free(props);
	if (proxy == NULL) {
		return NULL;
	}
	pb_link *l = calloc(1, sizeof(*l));
	if (l == NULL) {
		pw_proxy_destroy(proxy);
		return NULL;
	}
	l->conn = c;
	l->proxy = proxy;
	l->token = token;
	pw_proxy_add_listener(proxy, &l->proxy_listener, &link_proxy_events, l);
	spa_list_append(&c->links, &l->link);
	return l;
}

void pb_release_link(pb_link *l) {
	link_destroy(l);
}

int pb_destroy_global(pb_conn *c, uint32_t id) {
	return pw_registry_destroy(c->registry, id);
}

// test sinks

struct pw_proxy *pb_create_test_sink(pb_conn *c, const char *name) {
	struct pw_properties *props = pw_properties_new(
			"factory.name", "support.null-audio-sink",
			PW_KEY_MEDIA_CLASS, "Audio/Sink",
			PW_KEY_OBJECT_LINGER, "false",
			PW_KEY_PRIORITY_SESSION, "0",
			PW_KEY_PRIORITY_DRIVER, "0",
			"node.virtual", "true",
			"audio.position", "[ FL FR ]",
			"adapter.auto-port-config", "{ mode = dsp monitor = true position = preserve }",
			NULL);
	if (props == NULL) {
		return NULL;
	}
	pw_properties_set(props, PW_KEY_NODE_NAME, name);
	struct pw_proxy *p = pw_core_create_object(c->core, "adapter", PW_TYPE_INTERFACE_Node, PW_VERSION_NODE,
			&props->dict, 0);
	pw_properties_free(props);
	return p;
}

void pb_destroy_test_sink(struct pw_proxy *p) {
	pw_proxy_destroy(p);
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
