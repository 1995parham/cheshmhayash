package codec

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/1995parham/cheshmhayash/internal/schema"
)

// DefaultTypeHeader is the message header a producer can set to name the
// protobuf type of its payload ("ride.Accept" or "backend:ride.Accept").
// It wins over the subject rules. The "Nats-" prefix is reserved by the
// server, so the header does not use it.
const DefaultTypeHeader = "Proto-Type"

// ProtoDecoder decodes payloads whose type it can resolve: from Force,
// then from the type header, then from the subject rules.
type ProtoDecoder struct {
	Registry *schema.Registry
	Router   *schema.Router
	// TypeHeader overrides DefaultTypeHeader; "-" turns header lookup off.
	TypeHeader string
	// Force, when set, is the message reference for every payload.
	Force string
	// EmitDefaults renders fields that hold their zero value.
	EmitDefaults bool
}

// Name implements Decoder.
func (*ProtoDecoder) Name() string { return "protobuf" }

// Resolve returns the message reference for msg and where it came from
// ("--type", "header", or the matching subject pattern).
func (d *ProtoDecoder) Resolve(msg Message) (ref, origin string) {
	if d.Force != "" {
		return d.Force, "--type"
	}
	if h := d.typeHeader(); h != "" {
		if v := msg.Header.Get(h); v != "" {
			return v, "header " + h
		}
	}
	if rule, ok := d.Router.Resolve(msg.Subject); ok {
		return rule.Message, "subject " + rule.Pattern
	}
	return "", ""
}

func (d *ProtoDecoder) typeHeader() string {
	switch d.TypeHeader {
	case "-":
		return ""
	case "":
		return DefaultTypeHeader
	default:
		return d.TypeHeader
	}
}

// Decode implements Decoder.
func (d *ProtoDecoder) Decode(msg Message) (Result, bool, error) {
	ref, origin := d.Resolve(msg)
	if ref == "" || d.Registry == nil {
		return Result{}, false, nil
	}
	md, src, err := d.Registry.FindMessage(ref)
	if err != nil {
		return Result{}, false, fmt.Errorf("%w (from %s)", err, origin)
	}

	m := dynamicpb.NewMessage(md)
	opts := proto.UnmarshalOptions{Resolver: src.Types}
	if err := opts.Unmarshal(msg.Data, m); err != nil {
		return Result{}, false, fmt.Errorf("payload is not a %s (from %s): %w", md.FullName(), origin, err)
	}

	body, err := protojson.MarshalOptions{
		Resolver:        src.Types,
		UseProtoNames:   true,
		EmitUnpopulated: d.EmitDefaults,
	}.Marshal(m)
	if err != nil {
		return Result{}, false, fmt.Errorf("render %s: %w", md.FullName(), err)
	}

	res := Result{Format: FormatProto, Type: schema.Ref(src, md), Body: body}
	if n := unknownBytes(m.ProtoReflect()); n > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"%d bytes of fields that %s does not define: the producer may use a newer schema", n, md.FullName()))
	}
	return res, true, nil
}

// unknownBytes sums the unknown fields of m and of every message it holds.
func unknownBytes(m protoreflect.Message) int {
	n := len(m.GetUnknown())
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			if fd.MapValue().Message() != nil {
				v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
					n += unknownBytes(mv.Message())
					return true
				})
			}
		case fd.IsList():
			if fd.Message() != nil {
				l := v.List()
				for i := range l.Len() {
					n += unknownBytes(l.Get(i).Message())
				}
			}
		case fd.Message() != nil:
			n += unknownBytes(v.Message())
		}
		return true
	})
	return n
}

// ErrNoType reports an encode request whose message type is not known.
var ErrNoType = errors.New("no protobuf type: pass --type or add a subject rule")

// Encode turns JSON (protojson syntax: field names in either case, enums
// by name, Timestamps as RFC 3339) into the wire form of the message ref.
func Encode(reg *schema.Registry, ref string, jsonData []byte) ([]byte, protoreflect.MessageDescriptor, error) {
	if ref == "" {
		return nil, nil, ErrNoType
	}
	md, src, err := reg.FindMessage(ref)
	if err != nil {
		return nil, nil, err
	}
	m := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{Resolver: src.Types}).Unmarshal(jsonData, m); err != nil {
		return nil, nil, fmt.Errorf("JSON does not fit %s: %w", md.FullName(), err)
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		return nil, nil, fmt.Errorf("encode %s: %w", md.FullName(), err)
	}
	return data, md, nil
}
