package codec

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
)

// maxWireDepth bounds the nested-message guess, so a hostile payload cannot
// recurse without end.
const maxWireDepth = 32

// ProtoWireDecoder renders a protobuf payload without its schema: field
// numbers become keys, and each value is shown in the most likely form.
// It claims a payload only when the whole payload parses as protobuf.
type ProtoWireDecoder struct{}

// Name implements Decoder.
func (ProtoWireDecoder) Name() string { return "protowire" }

// Decode implements Decoder.
func (ProtoWireDecoder) Decode(msg Message) (Result, bool, error) {
	fields, ok := parseWire(msg.Data, 0)
	if !ok || len(fields) == 0 {
		return Result{}, false, nil
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return Result{}, false, err
	}
	return Result{
		Format: FormatProtoWire,
		Body:   body,
		Notes:  []string{"no schema for this subject: keys are field numbers and values are guesses"},
	}, true, nil
}

// wireFields keeps the fields of one message by number. A repeated field
// becomes a list.
type wireFields map[string]wireValue

// wireValue is one decoded field value: a number, a string, a nested
// message, or a list of those.
type wireValue struct {
	Num    *json.Number
	Str    *string
	Nested wireFields
	List   []wireValue
}

// MarshalJSON renders the value that is set.
func (v wireValue) MarshalJSON() ([]byte, error) {
	switch {
	case v.List != nil:
		return json.Marshal(v.List)
	case v.Nested != nil:
		return json.Marshal(v.Nested)
	case v.Num != nil:
		return json.Marshal(*v.Num)
	case v.Str != nil:
		return json.Marshal(*v.Str)
	default:
		return nil, errors.New("empty wire value")
	}
}

func parseWire(b []byte, depth int) (wireFields, bool) {
	if depth > maxWireDepth {
		return nil, false
	}
	fields := wireFields{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 || num < 1 {
			return nil, false
		}
		b = b[n:]

		var val wireValue
		switch typ {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return nil, false
			}
			val = number(strconv.FormatUint(v, 10))
			b = b[m:]
		case protowire.Fixed32Type:
			v, m := protowire.ConsumeFixed32(b)
			if m < 0 {
				return nil, false
			}
			val = fixed32(v)
			b = b[m:]
		case protowire.Fixed64Type:
			v, m := protowire.ConsumeFixed64(b)
			if m < 0 {
				return nil, false
			}
			val = fixed64(v)
			b = b[m:]
		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return nil, false
			}
			val = bytesValue(v, depth)
			b = b[m:]
		default:
			// Groups are deprecated and a strong sign that the payload is
			// not protobuf at all (a '{' byte reads as a group start).
			return nil, false
		}

		key := strconv.Itoa(int(num))
		if prev, seen := fields[key]; seen {
			if prev.List == nil {
				prev = wireValue{List: []wireValue{prev}}
			}
			prev.List = append(prev.List, val)
			fields[key] = prev
			continue
		}
		fields[key] = val
	}
	return fields, true
}

func number(s string) wireValue {
	n := json.Number(s)
	return wireValue{Num: &n}
}

// fixed32 shows a float when the bits look like a plain float, else the
// unsigned integer.
func fixed32(v uint32) wireValue {
	f := math.Float32frombits(v)
	if plausibleFloat(float64(f)) {
		return number(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	return number(strconv.FormatUint(uint64(v), 10))
}

// fixed64 shows a double when the bits look like a plain double (most
// coordinates and prices), else the unsigned integer.
func fixed64(v uint64) wireValue {
	f := math.Float64frombits(v)
	if plausibleFloat(f) {
		return number(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return number(strconv.FormatUint(v, 10))
}

func plausibleFloat(f float64) bool {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return false
	}
	a := math.Abs(f)
	return a == 0 || (a >= 1e-9 && a <= 1e15)
}

// bytesValue guesses what a length-delimited field holds: a nested
// message, text, or opaque bytes (base64).
func bytesValue(v []byte, depth int) wireValue {
	if len(v) > 0 && !isText(v) {
		if nested, ok := parseWire(v, depth+1); ok && len(nested) > 0 {
			return wireValue{Nested: nested}
		}
	}
	if isText(v) {
		s := string(v)
		return wireValue{Str: &s}
	}
	s := base64.StdEncoding.EncodeToString(v)
	return wireValue{Str: &s}
}
