package natsx

import (
	"encoding/json"
	"fmt"
)

// ListStreams paginates STREAM.LIST. Bound to the *connecting* account
// (so it fails with err_code 10039 when run on a $SYS connection that has
// no JetStream).
func (c *Cluster) ListStreams(offset uint64) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]uint64{"offset": offset})
	if err != nil {
		return nil, fmt.Errorf("encode list payload: %w", err)
	}
	return c.requestJSON("$JS.API.STREAM.LIST", payload)
}

// StreamInfo returns a single stream's info.
func (c *Cluster) StreamInfo(name string) (json.RawMessage, error) {
	return c.requestJSON("$JS.API.STREAM.INFO."+name, nil)
}

// UpdateStream PUTs a new StreamConfig. The payload must be a full config —
// NATS rejects partial updates. Some fields cannot change without recreate
// (storage, retention, etc.).
func (c *Cluster) UpdateStream(name string, config json.RawMessage) (json.RawMessage, error) {
	return c.requestJSON("$JS.API.STREAM.UPDATE."+name, config)
}

// PurgeStream drops every message from a stream (config and consumers stay).
func (c *Cluster) PurgeStream(name string) (json.RawMessage, error) {
	return c.requestJSON("$JS.API.STREAM.PURGE."+name, nil)
}

// DeleteStream removes the stream entirely.
func (c *Cluster) DeleteStream(name string) (json.RawMessage, error) {
	return c.requestJSON("$JS.API.STREAM.DELETE."+name, nil)
}

// ListConsumers paginates CONSUMER.LIST for a stream.
func (c *Cluster) ListConsumers(stream string, offset uint64) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]uint64{"offset": offset})
	if err != nil {
		return nil, fmt.Errorf("encode list payload: %w", err)
	}
	return c.requestJSON("$JS.API.CONSUMER.LIST."+stream, payload)
}

// ConsumerInfo returns one consumer's info.
func (c *Cluster) ConsumerInfo(stream, consumer string) (json.RawMessage, error) {
	return c.requestJSON(fmt.Sprintf("$JS.API.CONSUMER.INFO.%s.%s", stream, consumer), nil)
}

// DeleteConsumer removes a consumer.
func (c *Cluster) DeleteConsumer(stream, consumer string) (json.RawMessage, error) {
	return c.requestJSON(fmt.Sprintf("$JS.API.CONSUMER.DELETE.%s.%s", stream, consumer), nil)
}

// JSMOverview asks every server for a full JetStream report via
// $SYS.REQ.SERVER.PING.JSZ with {accounts, streams, consumer, config, raft}.
// With a system-account connection this yields cluster-wide visibility into
// every account, stream, and consumer the cluster knows about.
func (c *Cluster) JSMOverview() ([]json.RawMessage, error) {
	payload, err := json.Marshal(map[string]bool{
		"accounts": true,
		"streams":  true,
		"consumer": true,
		"config":   true,
		"raft":     true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode overview payload: %w", err)
	}
	return c.discover("$SYS.REQ.SERVER.PING.JSZ", payload, c.discoveryTimeout)
}
