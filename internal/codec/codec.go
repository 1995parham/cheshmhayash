// Package codec turns NATS payloads into JSON a person can read, and JSON
// back into payloads. Decoders form a chain: the first decoder that claims
// a payload renders it, so a protobuf schema wins over a JSON guess, which
// wins over text, then a schemaless protobuf dump, then raw bytes.
package codec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// Format names the encoding a payload was decoded from.
type Format string

// The formats the built-in decoders produce.
const (
	FormatProto     Format = "protobuf"
	FormatJSON      Format = "json"
	FormatProtoWire Format = "protowire"
	FormatText      Format = "text"
	FormatBinary    Format = "binary"
	FormatEmpty     Format = "empty"
)

// Header is the header set of a NATS message (nats.Header has this shape).
type Header map[string][]string

// Get returns the first value of key, or "".
func (h Header) Get(key string) string {
	if v := h[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// Message is one payload to decode.
type Message struct {
	Subject string
	Header  Header
	Data    []byte
}

// Result is a decoded payload.
type Result struct {
	// Format is the encoding the payload was read as.
	Format Format `json:"format"`
	// Type is the message reference ("source:full.Name") for protobuf.
	Type string `json:"type,omitempty"`
	// Body is the payload rendered as JSON. Text is a JSON string; binary
	// is a base64 JSON string.
	Body json.RawMessage `json:"payload"`
	// Notes explain a fallback or a partial decode, for example a schema
	// that did not fit or unknown fields.
	Notes []string `json:"notes,omitempty"`
}

// Decoder renders a payload, or declines it.
type Decoder interface {
	// Name identifies the decoder in notes and logs.
	Name() string
	// Decode returns ok=false when the decoder does not apply. A non-nil
	// error means the decoder applies but failed; the chain records the
	// error as a note and tries the next decoder.
	Decode(msg Message) (res Result, ok bool, err error)
}

// Chain tries each decoder in order.
type Chain []Decoder

// DefaultFallbacks returns the decoders that need no schema, in the order
// the chain should try them after any protobuf decoder.
func DefaultFallbacks() Chain {
	return Chain{JSONDecoder{}, TextDecoder{}, ProtoWireDecoder{}}
}

// Decode runs the chain. It always returns a result: when no decoder
// claims the payload, the bytes are rendered as base64.
func (c Chain) Decode(msg Message) Result {
	if len(msg.Data) == 0 {
		return Result{Format: FormatEmpty, Body: json.RawMessage(`""`)}
	}
	var notes []string
	for _, d := range c {
		res, ok, err := d.Decode(msg)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: %v", d.Name(), err))
			continue
		}
		if ok {
			res.Notes = append(notes, res.Notes...)
			return res
		}
	}
	return Result{Format: FormatBinary, Body: jsonString(base64.StdEncoding.EncodeToString(msg.Data)), Notes: notes}
}

// JSONDecoder claims payloads that are a JSON object or array.
type JSONDecoder struct{}

// Name implements Decoder.
func (JSONDecoder) Name() string { return "json" }

// Decode implements Decoder.
func (JSONDecoder) Decode(msg Message) (Result, bool, error) {
	first := firstNonSpace(msg.Data)
	if (first != '{' && first != '[') || !json.Valid(msg.Data) {
		return Result{}, false, nil
	}
	return Result{Format: FormatJSON, Body: json.RawMessage(msg.Data)}, true, nil
}

// TextDecoder claims payloads that are printable UTF-8.
type TextDecoder struct{}

// Name implements Decoder.
func (TextDecoder) Name() string { return "text" }

// Decode implements Decoder.
func (TextDecoder) Decode(msg Message) (Result, bool, error) {
	if !isText(msg.Data) {
		return Result{}, false, nil
	}
	return Result{Format: FormatText, Body: jsonString(string(msg.Data))}, true, nil
}

func firstNonSpace(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return c
		}
	}
	return 0
}

// isText reports whether b is valid UTF-8 without control characters
// other than whitespace.
func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
		if r == utf8.RuneError || r == 0x7f {
			return false
		}
	}
	return true
}

func jsonString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string fails only on invalid UTF-8, which it
		// replaces; this branch is unreachable in practice.
		return json.RawMessage(`""`)
	}
	return b
}
