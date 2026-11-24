package sparkplug

import (
	"fmt"
	"strings"
)

// Namespace is the Sparkplug B topic namespace.
const Namespace = "spBv1.0"

// Message types.
const (
	NBirth = "NBIRTH"
	NDeath = "NDEATH"
	NData  = "NDATA"
	NCmd   = "NCMD"
	DBirth = "DBIRTH"
	DData  = "DDATA"
	DCmd   = "DCMD"
)

// Topic builds spBv1.0/<group>/<type>/<edge>[/<device>].
func Topic(group, msgType, edge, device string) string {
	t := fmt.Sprintf("%s/%s/%s/%s", Namespace, group, msgType, edge)
	if device != "" {
		t += "/" + device
	}
	return t
}

// StateTopic is the primary host application's STATE topic.
func StateTopic(hostID string) string { return Namespace + "/STATE/" + hostID }

// ParsedTopic is a decoded Sparkplug topic.
type ParsedTopic struct {
	Group, Type, Edge, Device string
	State                     bool // STATE topic; Edge holds the host id
}

// ParseTopic decodes a Sparkplug topic.
func ParseTopic(t string) (ParsedTopic, bool) {
	parts := strings.Split(t, "/")
	if len(parts) < 3 || parts[0] != Namespace {
		return ParsedTopic{}, false
	}
	if parts[1] == "STATE" {
		return ParsedTopic{Type: "STATE", Edge: parts[2], State: true}, true
	}
	if len(parts) < 4 {
		return ParsedTopic{}, false
	}
	p := ParsedTopic{Group: parts[1], Type: parts[2], Edge: parts[3]}
	if len(parts) > 4 {
		p.Device = parts[4]
	}
	return p, true
}
