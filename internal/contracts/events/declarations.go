package events

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
)

type object map[string]json.RawMessage

func (o object) sub(key string) object {
	var result object
	_ = json.Unmarshal(o[key], &result)
	return result
}

func (o object) text(key string) string {
	var result string
	_ = json.Unmarshal(o[key], &result)
	return result
}

func (o object) list(key string) []json.RawMessage {
	var result []json.RawMessage
	_ = json.Unmarshal(o[key], &result)
	return result
}

func (o object) boolean(key string) *bool {
	var result *bool
	if json.Unmarshal(o[key], &result) != nil {
		return nil
	}
	return result
}

func (o object) integer(key string) *int {
	var result *int
	if json.Unmarshal(o[key], &result) != nil {
		return nil
	}
	return result
}

func node(raw json.RawMessage) object {
	var value object
	_ = json.Unmarshal(raw, &value)
	return value
}

var (
	brokerHost     = regexp.MustCompile(`^[A-Za-z0-9.-]{1,200}(:[0-9]{1,5})?$`)
	addressPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,240}$`)
	fieldPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

type channel struct {
	entity           contracts.Entity
	broker           Broker
	address          string
	messages         map[string]Message
	declaredMessages int
}

func (a *analyzer) asyncAPI(f Capture) error {
	doc := node(f.Bytes)
	if doc.text("asyncapi") != "3.0.0" {
		return a.gap("CONTRACT_ASYNCAPI_DIALECT_UNSUPPORTED", f.Ref)
	}
	if doc.sub("info").text("version") != f.Version {
		return a.gap("CONTRACT_ASYNCAPI_VERSION_MISMATCH", f.Ref)
	}
	servers := doc.sub("servers")
	rawChannels := doc.sub("channels")
	if (doc["channels"] != nil && rawChannels == nil) || (doc["operations"] != nil && doc.sub("operations") == nil) {
		return a.gap("CONTRACT_ASYNCAPI_SHAPE_UNSUPPORTED", f.Ref)
	}
	channels := make(map[string]channel)
	for _, key := range keys(rawChannels) {
		channel, err := a.channel(f, doc, servers, key, rawChannels.sub(key))
		if err != nil {
			return err
		}
		if channel.entity.Kind != "" {
			channels[key] = channel
		}
	}
	ops := doc.sub("operations")
	for _, key := range keys(ops) {
		if err := a.operation(f, key, ops.sub(key), channels); err != nil {
			return err
		}
	}
	return nil
}

func localName(ref object, prefix string) string {
	if len(ref) != 1 {
		return ""
	}
	name, ok := strings.CutPrefix(ref.text("$ref"), prefix)
	if !ok || !plainName.MatchString(name) {
		return ""
	}
	return name
}

func oneServer(doc object, servers object) (Broker, bool) {
	var selected string
	if doc["servers"] != nil {
		refs := doc.list("servers")
		if len(refs) != 1 {
			return Broker{}, false
		}
		selected = localName(node(refs[0]), "#/servers/")
	} else if len(servers) == 1 {
		for key := range servers {
			selected = key
		}
	}
	value := servers.sub(selected)
	server := Broker{Protocol: value.text("protocol"), Host: value.text("host"),
		Path: value.text("pathname"), Version: value.text("protocolVersion")}
	if !plainName.MatchString(selected) || !brokerHost.MatchString(server.Host) || value["variables"] != nil ||
		value["bindings"] != nil || value["security"] != nil ||
		value["$ref"] != nil || !plainName.MatchString(server.Version) ||
		server.Path != "" && (!strings.HasPrefix(server.Path, "/") || !addressPattern.MatchString(server.Path)) {
		return Broker{}, false
	}
	switch server.Protocol {
	case "amqp":
		return server, server.Version == "0.9.1"
	case "kafka":
		return server, true
	default:
		return Broker{}, false
	}
}

func transportKey(broker Broker, address string) string {
	return digest("event-transport-v1", broker.Protocol, broker.Version, broker.Host, broker.Path, address)
}

func (a *analyzer) channel(f Capture, full, servers object, key string, value object) (channel, error) {
	var ch channel
	address := value.text("address")
	if !plainName.MatchString(key) || !addressPattern.MatchString(address) ||
		value["parameters"] != nil || value["$ref"] != nil {
		return ch, a.gap("CONTRACT_EVENT_ADDRESS_UNRESOLVED", f.Ref)
	}
	server, ok := oneServer(value, servers)
	if !ok {
		return ch, a.gap("CONTRACT_EVENT_BROKER_UNRESOLVED", f.Ref)
	}
	if value["bindings"] != nil {
		return ch, a.gap("CONTRACT_EVENT_CHANNEL_BINDING_UNSUPPORTED", f.Ref)
	}
	pointer := "#/channels/" + key
	transport := transportKey(server, address)
	entity, err := a.entity(f, "asyncapi", "event_channel", transport, pointer)
	if err != nil {
		return ch, err
	}
	ch = channel{entity: entity, broker: server, address: address, messages: make(map[string]Message)}
	messages := value.sub("messages")
	ch.declaredMessages = len(messages)
	for _, msgName := range keys(messages) {
		msg, err := a.message(f, full, msgName, messages.sub(msgName), pointer, transport)
		if err != nil {
			return channel{}, err
		}
		if msg.Entity.Kind == "" {
			continue
		}
		ch.messages[msgName] = msg
		from, to := entity.Ref, msg.Entity.Ref
		if err := a.link(from, &to, to.EntityID, "channel_message", "syntax_exact", f.Ref); err != nil {
			return channel{}, err
		}
	}
	return ch, nil
}

// resolve follows only same-document component pointers in the capture.
func resolve(value object, components object, prefix string) (object, bool) {
	visited := make(map[string]bool)
	for depth := 0; depth < 16; depth++ {
		if value["$ref"] == nil {
			return value, value != nil
		}
		name := localName(value, prefix)
		if name == "" || visited[name] {
			return nil, false
		}
		visited[name] = true
		value = components.sub(name)
	}
	return nil, false
}

func (a *analyzer) message(f Capture, full object, name string, raw object, channelPointer, transport string) (Message, error) {
	var msg Message
	components := full.sub("components")
	value, ok := resolve(raw, components.sub("messages"), "#/components/messages/")
	if !ok || !plainName.MatchString(name) || value["traits"] != nil ||
		value["bindings"] != nil || value["headers"] != nil {
		return msg, a.gap("CONTRACT_EVENT_MESSAGE_UNRESOLVED", f.Ref)
	}
	codec := value.text("contentType")
	if value["contentType"] == nil {
		codec = full.text("defaultContentType")
	}
	version := value.text("x-mce4d-schema-version")
	if value["x-mce4d-schema-version"] == nil {
		version = f.Version
	}
	shape, ok := resolve(value.sub("payload"), components.sub("schemas"), "#/components/schemas/")
	if codec != "application/json" || !plainName.MatchString(version) || !ok {
		return msg, a.gap("CONTRACT_EVENT_CODEC_SCHEMA_UNRESOLVED", f.Ref)
	}
	fields, ok := scalarFields(shape)
	if !ok {
		return msg, a.gap("CONTRACT_EVENT_CODEC_SCHEMA_UNRESOLVED", f.Ref)
	}
	pointer := channelPointer + "/messages/" + name
	entity, err := a.entity(f, "asyncapi", "event_message", transport+"/"+name+"/"+version, pointer)
	if err != nil {
		return msg, err
	}
	for _, field := range keys(fields) {
		if err := a.field(entity.Ref, field, fields[field], "bidirectional", f); err != nil {
			return msg, err
		}
	}
	return Message{Entity: entity, Codec: codec, SchemaVersion: version}, nil
}

var scalars = map[string]bool{"null": true, "string": true, "boolean": true, "integer": true, "number": true}

func scalarFields(shape object) (map[string]string, bool) {
	for key := range shape {
		switch key {
		case "type", "properties", "description", "title", "required":
		default:
			return nil, false
		}
	}
	result := make(map[string]string)
	if typ := shape.text("type"); scalars[typ] && shape["properties"] == nil && shape["required"] == nil {
		result["/"] = typ
		return result, true
	}
	properties := shape.sub("properties")
	if shape.text("type") != "object" || shape["properties"] != nil && properties == nil {
		return nil, false
	}
	seenRequired := make(map[string]bool)
	if shape["required"] != nil && shape.list("required") == nil {
		return nil, false
	}
	for _, item := range shape.list("required") {
		var field string
		if json.Unmarshal(item, &field) != nil || properties[field] == nil || seenRequired[field] {
			return nil, false
		}
		seenRequired[field] = true
	}
	for field := range properties {
		value := properties.sub(field)
		typ := value.text("type")
		if !fieldPattern.MatchString(field) || !scalars[typ] {
			return nil, false
		}
		for key := range value {
			switch key {
			case "type", "description", "title":
			default:
				return nil, false
			}
		}
		result[field] = typ
	}
	return result, true
}

func (a *analyzer) operation(f Capture, name string, value object, channels map[string]channel) error {
	action := value.text("action")
	ref := localName(value.sub("channel"), "#/channels/")
	ch, known := channels[ref]
	if !plainName.MatchString(name) || !known || (action != "send" && action != "receive") ||
		value["$ref"] != nil || value["traits"] != nil || value["reply"] != nil {
		return a.gap("CONTRACT_EVENT_OPERATION_UNRESOLVED", f.Ref)
	}
	pointer := "#/operations/" + name
	entity, err := a.entity(f, "event", "event_"+action, transportKey(ch.broker, ch.address)+"/"+action+"/"+name, pointer)
	if err != nil {
		return err
	}
	op := Operation{Entity: entity, Broker: ch.broker, Address: ch.address, Action: action, Messages: []Message{}}
	op.Delivery, op.Bindable = delivery(f, pointer, value, op)
	if !op.Bindable {
		if err := a.gap("CONTRACT_EVENT_DELIVERY_UNRESOLVED", f.Ref); err != nil {
			return err
		}
	}
	ids := make(map[string]bool)
	expected := ch.declaredMessages
	if value["messages"] == nil {
		for id := range ch.messages {
			ids[id] = true
		}
	} else {
		subset := value.list("messages")
		expected = len(subset)
		if len(subset) == 0 {
			op.Bindable = false
		}
		for _, candidate := range subset {
			id := localName(node(candidate), "#/channels/"+ref+"/messages/")
			if id == "" || ch.messages[id].Entity.Kind == "" || ids[id] {
				op.Bindable = false
				continue
			}
			ids[id] = true
		}
	}
	if len(ids) == 0 || len(ids) != expected {
		op.Bindable = false
		if err := a.gap("CONTRACT_EVENT_MESSAGE_SET_UNRESOLVED", f.Ref); err != nil {
			return err
		}
	}
	target := ch.entity.Ref
	if err := a.link(entity.Ref, &target, target.EntityID, "operation_channel", "syntax_exact", f.Ref); err != nil {
		return err
	}
	for _, id := range keys(ids) {
		msg := ch.messages[id]
		target := msg.Entity.Ref
		if err := a.link(entity.Ref, &target, target.EntityID, "operation_message", "syntax_exact", f.Ref); err != nil {
			return err
		}
		op.Messages = append(op.Messages, msg)
	}
	a.out.Operations = append(a.out.Operations, op)
	return nil
}

func delivery(f Capture, pointer string, value object, op Operation) (Delivery, bool) {
	out := Delivery{Declaration: f.Ref, Pointer: pointer}
	ok := value["security"] == nil
	if value["bindings"] != nil {
		bindings := value.sub("bindings")
		amqp := bindings.sub("amqp")
		if len(bindings) != 1 || op.Broker.Protocol != "amqp" || amqp.text("bindingVersion") != "0.3.0" ||
			len(amqp) != 2 || op.Action != "receive" || amqp.boolean("ack") == nil {
			ok = false
		} else {
			out.Ack = amqp.boolean("ack")
		}
	}
	if value["x-mce4d-delivery"] != nil {
		cfg := value.sub("x-mce4d-delivery")
		if cfg == nil {
			return out, false
		}
		for field := range cfg {
			switch field {
			case "retry_limit":
				v := cfg.integer(field)
				if v == nil || *v < 0 || *v > 100000 {
					ok = false
				} else {
					out.RetryLimit = v
				}
			case "replay":
				out.Replay = cfg.boolean(field)
				ok = ok && out.Replay != nil
			case "dead_letter", "deduplication", "outbox", "inbox":
				v := cfg.text(field)
				if !addressPattern.MatchString(v) {
					ok = false
					continue
				}
				switch field {
				case "dead_letter":
					out.DeadLetter = v
				case "deduplication":
					out.Deduplication = v
				case "outbox":
					out.Outbox = v
				case "inbox":
					out.Inbox = v
				}
			default:
				ok = false
			}
		}
	}
	return out, ok
}

func (a *analyzer) cache(f Capture) error {
	const kind = "redis-cache-v1"
	doc := node(f.Bytes)
	backend := doc.sub("backend")
	server, database := backend.text("instance"), backend.integer("database")
	if doc.text("format") != kind || doc.text("version") != f.Version ||
		doc.text("namespace") != a.profile.Namespace || !plainName.MatchString(server) ||
		database == nil || *database < 0 || *database > 65535 || len(backend) != 2 || len(doc) != 5 {
		return a.gap("CONTRACT_CACHE_SCOPE_UNRESOLVED", f.Ref)
	}
	entries := doc.sub("entries")
	if entries == nil {
		return a.gap("CONTRACT_CACHE_ENTRIES_UNRESOLVED", f.Ref)
	}
	for _, id := range keys(entries) {
		value := entries.sub(id)
		key, typ, ttl := value.text("key"), value.text("type"), value.integer("ttl_seconds")
		prefix := a.profile.Namespace + ":"
		if !plainName.MatchString(id) || !strings.HasPrefix(key, prefix) ||
			!addressPattern.MatchString(strings.TrimPrefix(key, prefix)) || !scalars[typ] ||
			len(value) != 2 && len(value) != 3 ||
			len(value) == 3 && (ttl == nil || *ttl <= 0 || *ttl > 31536000) {
			if err := a.gap("CONTRACT_CACHE_KEY_OR_TTL_UNRESOLVED", f.Ref); err != nil {
				return err
			}
			continue
		}
		identity := fmt.Sprintf("%s/%d/%s", server, *database, key)
		entity, err := a.entity(f, "cache", "cache_key", identity, "#/entries/"+id)
		if err != nil {
			return err
		}
		if err := a.field(entity.Ref, "value", typ, "stored", f); err != nil {
			return err
		}
		a.out.Cache = append(a.out.Cache, CacheEntry{Entity: entity, BackendID: server, Database: *database,
			Key: key, Type: typ, TTLSeconds: ttl})
	}
	return nil
}
