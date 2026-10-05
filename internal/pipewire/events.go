package pipewire

// Event is a discrete change the ui reacts to rather than diffs for. events are delivered after the snapshot that
// contains them has been published.
type Event interface {
	event()
}

// ObjectKind names the kind of an observed object.
type ObjectKind int

const (
	KindNode ObjectKind = iota
	KindPort
	KindLink
	KindDevice
	KindClient
	KindMetadata
)

func (k ObjectKind) String() string {
	switch k {
	case KindNode:
		return "node"
	case KindPort:
		return "port"
	case KindLink:
		return "link"
	case KindDevice:
		return "device"
	case KindClient:
		return "client"
	case KindMetadata:
		return "metadata"
	default:
		return "unknown"
	}
}

// ObjectAppeared reports a newly observed object.
type ObjectAppeared struct {
	Kind   ObjectKind
	Serial Serial
}

// ObjectVanished reports the removal of an observed object.
type ObjectVanished struct {
	Kind   ObjectKind
	Serial Serial
}

// LinkStateChanged reports a link's bound info state, including its first.
type LinkStateChanged struct {
	Serial Serial
	State  string
	Error  string
}

// ConnStateChanged reports a change of connection state.
type ConnStateChanged struct {
	State ConnState
	Error string
}

// RequestID identifies a request posted to the backend; its outcome arrives only as a RequestResolved event.
type RequestID uint64

// RequestResolved reports the outcome of a request.
type RequestResolved struct {
	ID     RequestID
	OK     bool
	Reason string
}

func (ObjectAppeared) event()   {}
func (ObjectVanished) event()   {}
func (LinkStateChanged) event() {}
func (ConnStateChanged) event() {}
func (RequestResolved) event()  {}
