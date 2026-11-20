package codec

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/1995parham/cheshmhayash/internal/schema"
)

func testRegistry(t *testing.T) *schema.Registry {
	t.Helper()
	reg, err := schema.Load(t.Context(), []schema.SourceConfig{
		{Name: "backend", Type: schema.TypeProtoDir, Path: "../schema/testdata/backend"},
		{Name: "map", Type: schema.TypeProtoDir, Path: "../schema/testdata/mapsvc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func testChain(t *testing.T, reg *schema.Registry, force string) Chain {
	t.Helper()
	router, err := schema.NewRouter([]schema.Rule{
		{Pattern: "demo.ping.>", Message: "backend:demo.v1.Ping"},
		{Pattern: "demo.pong", Message: "demo.v1.Pong"},
		{Pattern: "demo.broken", Message: "demo.v1.Missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(Chain{&ProtoDecoder{Registry: reg, Router: router, Force: force}}, DefaultFallbacks()...)
}

func encodePing(t *testing.T, reg *schema.Registry) []byte {
	t.Helper()
	data, _, err := Encode(reg, "backend:demo.v1.Ping",
		[]byte(`{"id":"r-1","count":"5","region":"REGION_EU","at":"2026-11-20T10:00:00Z","inner":{"ok":true},"tags":{"a":1}}`))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data
}

func decodeBody(t *testing.T, res Result) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(res.Body, &body); err != nil {
		t.Fatalf("body %s is not a JSON object: %v", res.Body, err)
	}
	return body
}

func TestProtoBySubject(t *testing.T) {
	reg := testRegistry(t)
	res := testChain(t, reg, "").Decode(Message{Subject: "demo.ping.eu1", Data: encodePing(t, reg)})

	if res.Format != FormatProto || res.Type != "backend:demo.v1.Ping" {
		t.Fatalf("got %s %s, want protobuf backend:demo.v1.Ping (notes %v)", res.Format, res.Type, res.Notes)
	}
	body := decodeBody(t, res)
	for key, want := range map[string]string{
		"id": `"r-1"`, "count": `"5"`, "region": `"REGION_EU"`, "at": `"2026-11-20T10:00:00Z"`,
	} {
		if got := string(body[key]); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if len(res.Notes) != 0 {
		t.Errorf("unexpected notes %v", res.Notes)
	}
}

func TestProtoTypePrecedence(t *testing.T) {
	reg := testRegistry(t)
	pong, _, err := Encode(reg, "demo.v1.Pong", []byte(`{"id":"p"}`))
	if err != nil {
		t.Fatal(err)
	}

	// The header wins over the subject rule.
	res := testChain(t, reg, "").Decode(Message{
		Subject: "demo.ping.x",
		Header:  Header{DefaultTypeHeader: {"map:demo.v1.Pong"}},
		Data:    pong,
	})
	if res.Type != "map:demo.v1.Pong" {
		t.Fatalf("header: type = %q, want map:demo.v1.Pong", res.Type)
	}

	// --type wins over the header.
	res = testChain(t, reg, "map:demo.v1.Ping").Decode(Message{
		Subject: "demo.ping.x",
		Header:  Header{DefaultTypeHeader: {"map:demo.v1.Pong"}},
		Data:    pong,
	})
	if res.Type != "map:demo.v1.Ping" {
		t.Fatalf("force: type = %q, want map:demo.v1.Ping", res.Type)
	}
}

func TestProtoUnknownFieldsNote(t *testing.T) {
	reg := testRegistry(t)
	data := encodePing(t, reg)
	data = protowire.AppendTag(data, 99, protowire.VarintType)
	data = protowire.AppendVarint(data, 7)

	res := testChain(t, reg, "").Decode(Message{Subject: "demo.ping.x", Data: data})
	if res.Format != FormatProto || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "does not define") {
		t.Fatalf("got %s with notes %v, want protobuf with an unknown-fields note", res.Format, res.Notes)
	}
}

func TestProtoFallbacks(t *testing.T) {
	reg := testRegistry(t)
	chain := testChain(t, reg, "")

	tests := []struct {
		name       string
		msg        Message
		wantFormat Format
		wantNote   string
	}{
		{
			name:       "schema does not fit",
			msg:        Message{Subject: "demo.ping.x", Data: []byte{0xff, 0xff, 0xff}},
			wantFormat: FormatBinary,
			wantNote:   "payload is not a demo.v1.Ping",
		},
		{
			name:       "rule names a missing message",
			msg:        Message{Subject: "demo.broken", Data: []byte(`{"a":1}`)},
			wantFormat: FormatJSON,
			wantNote:   "message not found",
		},
		{name: "json without rule", msg: Message{Subject: "x", Data: []byte(` {"a":[1,2]}`)}, wantFormat: FormatJSON},
		{name: "text", msg: Message{Subject: "x", Data: []byte("hello, nats\n")}, wantFormat: FormatText},
		{name: "number is text", msg: Message{Subject: "x", Data: []byte("42")}, wantFormat: FormatText},
		{name: "empty", msg: Message{Subject: "x"}, wantFormat: FormatEmpty},
		{name: "binary", msg: Message{Subject: "x", Data: []byte{0x00, 0x01, 0xfe, 0xff}}, wantFormat: FormatBinary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := chain.Decode(tt.msg)
			if res.Format != tt.wantFormat {
				t.Fatalf("format = %s, want %s (notes %v)", res.Format, tt.wantFormat, res.Notes)
			}
			if tt.wantNote != "" && (len(res.Notes) == 0 || !strings.Contains(res.Notes[0], tt.wantNote)) {
				t.Fatalf("notes = %v, want one containing %q", res.Notes, tt.wantNote)
			}
			if !json.Valid(res.Body) {
				t.Fatalf("body %q is not valid JSON", res.Body)
			}
		})
	}
}

func TestProtoWireWithoutSchema(t *testing.T) {
	reg := testRegistry(t)
	res := DefaultFallbacks().Decode(Message{Subject: "unknown", Data: encodePing(t, reg)})
	if res.Format != FormatProtoWire {
		t.Fatalf("format = %s, want protowire", res.Format)
	}
	body := decodeBody(t, res)
	if got := string(body["1"]); got != `"r-1"` {
		t.Errorf("field 1 = %s, want \"r-1\"", got)
	}
	if got := string(body["2"]); got != "5" {
		t.Errorf("field 2 = %s, want 5", got)
	}
	if got := string(body["4"]); got != `{"1":1}` {
		t.Errorf("field 4 = %s, want the nested message {\"1\":1}", got)
	}
}

func TestProtoWireRepeatedAndFixed(t *testing.T) {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.Fixed64Type)
	b = protowire.AppendFixed64(b, math.Float64bits(35.701)) // a latitude
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, 1)
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, 2)

	res := ProtoWireDecoder{}
	got, ok, err := res.Decode(Message{Data: b})
	if err != nil || !ok {
		t.Fatalf("Decode = %v, %v", ok, err)
	}
	body := decodeBody(t, got)
	if string(body["1"]) != "35.701" || string(body["2"]) != "[1,2]" {
		t.Fatalf("body = %s", got.Body)
	}
}

func TestEncodeErrors(t *testing.T) {
	reg := testRegistry(t)
	if _, _, err := Encode(reg, "", []byte(`{}`)); err == nil {
		t.Error("empty type accepted")
	}
	if _, _, err := Encode(reg, "demo.v1.Ping", []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "several sources") {
		t.Errorf("ambiguous type: err = %v", err)
	}
	if _, _, err := Encode(reg, "map:demo.v1.Pong", []byte(`{"nope":1}`)); err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Errorf("unknown field: err = %v", err)
	}
}
