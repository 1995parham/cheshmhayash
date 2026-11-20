package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/1995parham/cheshmhayash/internal/codec"
	protoschema "github.com/1995parham/cheshmhayash/internal/schema"
)

// Bounds of subject_sample, so one tool call cannot hold a subscription
// open for long or flood the model's context.
const (
	defaultSampleCount   = 5
	maxSampleCount       = 50
	defaultSampleTimeout = 5 * time.Second
	maxSampleTimeout     = 30 * time.Second
)

// ChainFactory builds the decoder chain for one call; force, when set, is
// the protobuf type for every payload.
type ChainFactory func(force string) codec.Chain

// EnableSampling registers the subject_sample tool, which subscribes to a
// subject for a moment and returns the messages decoded. It sends message
// content to the model, so the operator turns it on explicitly.
func (s *Server) EnableSampling(chains ChainFactory) {
	s.chains = chains
	s.tools = append(s.tools, tool{
		name: "subject_sample",
		description: "Subscribe to a subject on core NATS for a short time and return up to `count` " +
			"messages with decoded payloads (protobuf by the configured schemas, else JSON, text, a " +
			"schemaless protobuf dump, or base64). Wildcards * and > work. It creates no JetStream " +
			"consumer. The connection's account decides which messages are visible: a system-account " +
			"connection sees only $SYS subjects.",
		inputSchema: schema(map[string]any{
			"cluster":         stringProp("Configured cluster name. See cluster_list."),
			"subject":         stringProp("Subject or wildcard pattern, e.g. ride.accepted or ride.>."),
			"count":           intProp(fmt.Sprintf("Messages to return (default %d, at most %d).", defaultSampleCount, maxSampleCount)),
			"timeout_seconds": intProp(fmt.Sprintf("Stop waiting after this long (default %d, at most %d).", int(defaultSampleTimeout.Seconds()), int(maxSampleTimeout.Seconds()))),
			"type":            stringProp("Optional protobuf message to decode every payload as (\"source:full.Name\")."),
		}, []string{"cluster", "subject"}),
		handler: toolSubjectSample,
	})
}

type sampleArgs struct {
	Cluster        string `json:"cluster"`
	Subject        string `json:"subject"`
	Count          int    `json:"count"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Type           string `json:"type"`
}

type sampledMessage struct {
	Subject string          `json:"subject"`
	Size    int             `json:"size"`
	Headers codec.Header    `json:"headers,omitempty"`
	Format  codec.Format    `json:"format"`
	Type    string          `json:"type,omitempty"`
	Notes   []string        `json:"notes,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type sampleResult struct {
	Subject  string           `json:"subject"`
	Received int              `json:"received"`
	TimedOut bool             `json:"timed_out"`
	Messages []sampledMessage `json:"messages"`
}

func toolSubjectSample(ctx context.Context, s *Server, args json.RawMessage) (string, error) {
	var in sampleArgs
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if err := protoschema.ValidatePattern(in.Subject); err != nil {
		return "", fmt.Errorf("subject %q: %w", in.Subject, err)
	}
	count := clamp(in.Count, defaultSampleCount, maxSampleCount)
	timeout := time.Duration(clamp(in.TimeoutSeconds, int(defaultSampleTimeout.Seconds()), int(maxSampleTimeout.Seconds()))) * time.Second

	c, err := s.resolveCluster(in.Cluster)
	if err != nil {
		return "", err
	}
	chain := s.chains(in.Type)

	sub, err := c.Conn().SubscribeSync(in.Subject)
	if err != nil {
		return "", fmt.Errorf("subscribe %s: %w", in.Subject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := sampleResult{Subject: in.Subject, Messages: []sampledMessage{}}
	for res.Received < count {
		m, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				res.TimedOut = true
				break
			}
			return "", fmt.Errorf("receive: %w", err)
		}
		res.Received++
		msg := codec.Message{Subject: m.Subject, Header: codec.Header(m.Header), Data: m.Data}
		d := chain.Decode(msg)
		res.Messages = append(res.Messages, sampledMessage{
			Subject: m.Subject, Size: len(m.Data), Headers: msg.Header,
			Format: d.Format, Type: d.Type, Notes: d.Notes, Payload: d.Body,
		})
	}

	out, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// clamp returns def for a non-positive v, and caps v at maxV.
func clamp(v, def, maxV int) int {
	if v <= 0 {
		return def
	}
	return min(v, maxV)
}
