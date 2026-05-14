package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

// tool is one MCP tool entry: the JSON-Schema the client sees plus the
// handler that runs against natsx. Handlers return the text payload sent
// back to the model (already valid JSON in our case).
type tool struct {
	name        string
	description string
	inputSchema map[string]any
	handler     func(ctx context.Context, s *Server, args json.RawMessage) (string, error)
}

func buildTools(write bool) []tool {
	tools := []tool{
		// ----- meta / cluster ------------------------------------------
		{
			name:        "cluster_list",
			description: "List the configured NATS cluster names this server can reach.",
			inputSchema: schema(nil, nil),
			handler:     toolClusterList,
		},

		// ----- $SYS.REQ.SERVER (admin / read) --------------------------
		{
			name: "server_ping",
			description: "Fan-out $SYS.REQ.SERVER.PING and return every server's reply. " +
				"Use this to enumerate servers in a cluster.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name. See cluster_list."),
			}, []string{"cluster"}),
			handler: toolServerPing,
		},
		{
			name: "server_ping_endpoint",
			description: "Fan-out $SYS.REQ.SERVER.PING.<endpoint>. Endpoint is one of " +
				strings.Join(natsx.ServerEndpoints, ", ") +
				" (case-insensitive). VARZ for config/uptime, CONNZ for clients, " +
				"JSZ for JetStream, HEALTHZ for liveness.",
			inputSchema: schema(map[string]any{
				"cluster":  stringProp("Configured cluster name."),
				"endpoint": enumProp("Monitoring endpoint suffix.", natsx.ServerEndpoints),
			}, []string{"cluster", "endpoint"}),
			handler: toolServerPingEndpoint,
		},
		{
			name: "server_endpoint",
			description: "Targeted $SYS.REQ.SERVER.<id>.<endpoint>. Returns one reply " +
				"from the named server (typically the server_id from server_ping).",
			inputSchema: schema(map[string]any{
				"cluster":   stringProp("Configured cluster name."),
				"server_id": stringProp("NATS server_id (UUID-ish; see server_ping)."),
				"endpoint":  enumProp("Monitoring endpoint suffix.", natsx.ServerEndpoints),
			}, []string{"cluster", "server_id", "endpoint"}),
			handler: toolServerEndpoint,
		},
		{
			name: "account_endpoint",
			description: "Fan-out $SYS.REQ.ACCOUNT.<account>.<endpoint>. Endpoint is one of " +
				strings.Join(natsx.AccountEndpoints, ", ") +
				". Requires a system-account connection.",
			inputSchema: schema(map[string]any{
				"cluster":  stringProp("Configured cluster name."),
				"account":  stringProp("Account name (e.g. $G, $SYS, your tenant)."),
				"endpoint": enumProp("Account endpoint suffix.", natsx.AccountEndpoints),
			}, []string{"cluster", "account", "endpoint"}),
			handler: toolAccountEndpoint,
		},

		// ----- $JS.API (JetStream / read) ------------------------------
		{
			name: "jsm_overview",
			description: "Cluster-wide JetStream overview — fans out " +
				"$SYS.REQ.SERVER.PING.JSZ with accounts/streams/consumer/config/raft=true. " +
				"One reply per server. Heavy payload on busy clusters.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
			}, []string{"cluster"}),
			handler: toolJSMOverview,
		},
		{
			name:        "stream_list",
			description: "Paginated $JS.API.STREAM.LIST for the account the cluster's credentials are bound to.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
				"offset":  intProp("Pagination offset; defaults to 0."),
			}, []string{"cluster"}),
			handler: toolStreamList,
		},
		{
			name:        "stream_info",
			description: "$JS.API.STREAM.INFO.<stream> — full stream state including replicas, consumers, and storage.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
				"stream":  stringProp("Stream name."),
			}, []string{"cluster", "stream"}),
			handler: toolStreamInfo,
		},
		{
			name:        "consumer_list",
			description: "Paginated $JS.API.CONSUMER.LIST.<stream>.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
				"stream":  stringProp("Stream name."),
				"offset":  intProp("Pagination offset; defaults to 0."),
			}, []string{"cluster", "stream"}),
			handler: toolConsumerList,
		},
		{
			name:        "consumer_info",
			description: "$JS.API.CONSUMER.INFO.<stream>.<consumer> — durable name, ack pending, redeliveries.",
			inputSchema: schema(map[string]any{
				"cluster":  stringProp("Configured cluster name."),
				"stream":   stringProp("Stream name."),
				"consumer": stringProp("Consumer name."),
			}, []string{"cluster", "stream", "consumer"}),
			handler: toolConsumerInfo,
		},
	}

	if !write {
		return tools
	}

	// ----- mutating tools (gated by CHESHMHAYASH_MCP_WRITE) -----------
	tools = append(tools,
		tool{
			name:        "server_reload",
			description: "Trigger a config reload on a single server ($SYS.REQ.SERVER.<id>.RELOAD).",
			inputSchema: schema(map[string]any{
				"cluster":   stringProp("Configured cluster name."),
				"server_id": stringProp("Target server_id."),
			}, []string{"cluster", "server_id"}),
			handler: toolServerReload,
		},
		tool{
			name: "server_lame_duck",
			description: "Place a server into lame-duck mode (graceful drain). " +
				"Connections move off to peers; new ones are refused.",
			inputSchema: schema(map[string]any{
				"cluster":   stringProp("Configured cluster name."),
				"server_id": stringProp("Target server_id."),
				"confirm":   confirmProp(),
			}, []string{"cluster", "server_id", "confirm"}),
			handler: toolServerLameDuck,
		},
		tool{
			name:        "server_kick",
			description: "Forcibly disconnect one client by CID ($SYS.REQ.SERVER.<id>.KICK).",
			inputSchema: schema(map[string]any{
				"cluster":   stringProp("Configured cluster name."),
				"server_id": stringProp("Target server_id."),
				"cid":       intProp("Client connection id from CONNZ."),
				"confirm":   confirmProp(),
			}, []string{"cluster", "server_id", "cid", "confirm"}),
			handler: toolServerKick,
		},
		tool{
			name: "stream_update",
			description: "PUT a full StreamConfig to $JS.API.STREAM.UPDATE.<stream>. " +
				"NATS rejects partial updates — pass the entire config. Some fields " +
				"(storage, retention) cannot be changed without recreate.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
				"stream":  stringProp("Stream name (must match config.name if present)."),
				"config":  objectProp("Full StreamConfig JSON object."),
			}, []string{"cluster", "stream", "config"}),
			handler: toolStreamUpdate,
		},
		tool{
			name:        "stream_purge",
			description: "Drop every message from a stream ($JS.API.STREAM.PURGE). Config and consumers stay.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
				"stream":  stringProp("Stream name."),
				"confirm": confirmProp(),
			}, []string{"cluster", "stream", "confirm"}),
			handler: toolStreamPurge,
		},
		tool{
			name:        "stream_delete",
			description: "Delete a stream entirely ($JS.API.STREAM.DELETE). Irreversible.",
			inputSchema: schema(map[string]any{
				"cluster": stringProp("Configured cluster name."),
				"stream":  stringProp("Stream name."),
				"confirm": confirmProp(),
			}, []string{"cluster", "stream", "confirm"}),
			handler: toolStreamDelete,
		},
		tool{
			name:        "consumer_delete",
			description: "Delete a consumer ($JS.API.CONSUMER.DELETE).",
			inputSchema: schema(map[string]any{
				"cluster":  stringProp("Configured cluster name."),
				"stream":   stringProp("Stream name."),
				"consumer": stringProp("Consumer name."),
				"confirm":  confirmProp(),
			}, []string{"cluster", "stream", "consumer", "confirm"}),
			handler: toolConsumerDelete,
		},
	)
	return tools
}

// ----- schema helpers --------------------------------------------------

func schema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	out := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 0, "description": desc}
}

func objectProp(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
}

func enumProp(desc string, values []string) map[string]any {
	vs := make([]any, 0, len(values))
	for _, v := range values {
		vs = append(vs, v)
	}
	return map[string]any{"type": "string", "description": desc, "enum": vs}
}

func confirmProp() map[string]any {
	return map[string]any{
		"type":  "boolean",
		"const": true,
		"description": "Must be true to actually run the destructive action. " +
			"Acts as the same safety gate as ?confirm=true on the HTTP API.",
	}
}

// ----- shared decode helpers ------------------------------------------

func decodeArgs(args json.RawMessage, into any) error {
	if len(args) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("decode arguments: %w", err)
	}
	return nil
}

// rawArray collapses a slice of raw replies into a single JSON array
// string, mirroring writeRawArray in the HTTP handler.
func rawArray(items []json.RawMessage) string {
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(it)
	}
	b.WriteByte(']')
	return b.String()
}

// ----- tool implementations --------------------------------------------

func toolClusterList(_ context.Context, s *Server, _ json.RawMessage) (string, error) {
	names := s.mgr.Names()
	buf, err := json.Marshal(names)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

type clusterArg struct {
	Cluster string `json:"cluster"`
}

func toolServerPing(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a clusterArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.Ping()
	if err != nil {
		return "", err
	}
	return rawArray(out), nil
}

type pingEndpointArg struct {
	Cluster  string `json:"cluster"`
	Endpoint string `json:"endpoint"`
}

func toolServerPingEndpoint(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a pingEndpointArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !natsx.IsServerEndpoint(a.Endpoint) {
		return "", fmt.Errorf("unknown endpoint %q; valid: %s", a.Endpoint, strings.Join(natsx.ServerEndpoints, ", "))
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.PingEndpoint(a.Endpoint)
	if err != nil {
		return "", err
	}
	return rawArray(out), nil
}

type serverEndpointArg struct {
	Cluster  string `json:"cluster"`
	ServerID string `json:"server_id"`
	Endpoint string `json:"endpoint"`
}

func toolServerEndpoint(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a serverEndpointArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.ServerID == "" {
		return "", errors.New("server_id is required")
	}
	if !natsx.IsServerEndpoint(a.Endpoint) {
		return "", fmt.Errorf("unknown endpoint %q; valid: %s", a.Endpoint, strings.Join(natsx.ServerEndpoints, ", "))
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.ServerEndpoint(a.ServerID, a.Endpoint)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type accountEndpointArg struct {
	Cluster  string `json:"cluster"`
	Account  string `json:"account"`
	Endpoint string `json:"endpoint"`
}

func toolAccountEndpoint(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a accountEndpointArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.Account == "" {
		return "", errors.New("account is required")
	}
	if !natsx.IsAccountEndpoint(a.Endpoint) {
		return "", fmt.Errorf("unknown endpoint %q; valid: %s", a.Endpoint, strings.Join(natsx.AccountEndpoints, ", "))
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.AccountEndpoint(a.Account, a.Endpoint)
	if err != nil {
		return "", err
	}
	return rawArray(out), nil
}

func toolJSMOverview(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a clusterArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.JSMOverview()
	if err != nil {
		return "", err
	}
	return rawArray(out), nil
}

type streamListArg struct {
	Cluster string `json:"cluster"`
	Offset  uint64 `json:"offset"`
}

func toolStreamList(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a streamListArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.ListStreams(a.Offset)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type streamArg struct {
	Cluster string `json:"cluster"`
	Stream  string `json:"stream"`
}

func toolStreamInfo(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a streamArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.Stream == "" {
		return "", errors.New("stream is required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.StreamInfo(a.Stream)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type consumerListArg struct {
	Cluster string `json:"cluster"`
	Stream  string `json:"stream"`
	Offset  uint64 `json:"offset"`
}

func toolConsumerList(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a consumerListArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.Stream == "" {
		return "", errors.New("stream is required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.ListConsumers(a.Stream, a.Offset)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type consumerArg struct {
	Cluster  string `json:"cluster"`
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
}

func toolConsumerInfo(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a consumerArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.Stream == "" || a.Consumer == "" {
		return "", errors.New("stream and consumer are required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.ConsumerInfo(a.Stream, a.Consumer)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ----- mutating tools -------------------------------------------------

type serverIDArg struct {
	Cluster  string `json:"cluster"`
	ServerID string `json:"server_id"`
}

func toolServerReload(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a serverIDArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.ServerID == "" {
		return "", errors.New("server_id is required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.Reload(a.ServerID)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type serverIDConfirmArg struct {
	Cluster  string `json:"cluster"`
	ServerID string `json:"server_id"`
	Confirm  bool   `json:"confirm"`
}

func toolServerLameDuck(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a serverIDConfirmArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !a.Confirm {
		return "", errors.New("confirm must be true to lame-duck a server")
	}
	if a.ServerID == "" {
		return "", errors.New("server_id is required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.LameDuck(a.ServerID)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type kickArg struct {
	Cluster  string `json:"cluster"`
	ServerID string `json:"server_id"`
	CID      uint64 `json:"cid"`
	Confirm  bool   `json:"confirm"`
}

func toolServerKick(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a kickArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !a.Confirm {
		return "", errors.New("confirm must be true to kick a client")
	}
	if a.ServerID == "" || a.CID == 0 {
		return "", errors.New("server_id and cid are required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.Kick(a.ServerID, a.CID)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type streamUpdateArg struct {
	Cluster string                     `json:"cluster"`
	Stream  string                     `json:"stream"`
	Config  map[string]json.RawMessage `json:"config"`
}

func toolStreamUpdate(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a streamUpdateArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if a.Stream == "" || a.Config == nil {
		return "", errors.New("stream and config are required")
	}
	if rawName, ok := a.Config["name"]; ok {
		var n string
		if err := json.Unmarshal(rawName, &n); err != nil {
			return "", errors.New("config.name must be a string")
		}
		if n != a.Stream {
			return "", fmt.Errorf("config.name %q does not match stream %q", n, a.Stream)
		}
	} else {
		a.Config["name"], _ = json.Marshal(a.Stream)
	}
	payload, err := json.Marshal(a.Config)
	if err != nil {
		return "", fmt.Errorf("re-encode config: %w", err)
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.UpdateStream(a.Stream, payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type streamConfirmArg struct {
	Cluster string `json:"cluster"`
	Stream  string `json:"stream"`
	Confirm bool   `json:"confirm"`
}

func toolStreamPurge(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a streamConfirmArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !a.Confirm {
		return "", errors.New("confirm must be true to purge a stream")
	}
	if a.Stream == "" {
		return "", errors.New("stream is required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.PurgeStream(a.Stream)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func toolStreamDelete(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a streamConfirmArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !a.Confirm {
		return "", errors.New("confirm must be true to delete a stream")
	}
	if a.Stream == "" {
		return "", errors.New("stream is required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.DeleteStream(a.Stream)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type consumerConfirmArg struct {
	Cluster  string `json:"cluster"`
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
	Confirm  bool   `json:"confirm"`
}

func toolConsumerDelete(_ context.Context, s *Server, args json.RawMessage) (string, error) {
	var a consumerConfirmArg
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !a.Confirm {
		return "", errors.New("confirm must be true to delete a consumer")
	}
	if a.Stream == "" || a.Consumer == "" {
		return "", errors.New("stream and consumer are required")
	}
	c, err := s.resolveCluster(a.Cluster)
	if err != nil {
		return "", err
	}
	out, err := c.DeleteConsumer(a.Stream, a.Consumer)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
