package schema

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func loadTestRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := Load(t.Context(), []SourceConfig{
		{Name: "backend", Type: TypeProtoDir, Path: "testdata/backend"},
		{Name: "map", Type: TypeProtoDir, Path: "testdata/mapsvc"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return reg
}

func TestFindMessage(t *testing.T) {
	reg := loadTestRegistry(t)

	tests := []struct {
		ref        string
		wantSource string
		wantErr    string
	}{
		{ref: "backend:demo.v1.Ping", wantSource: "backend"},
		{ref: "map:demo.v1.Ping", wantSource: "map"},
		{ref: "demo.v1.Pong", wantSource: "map"},
		{ref: "demo.v1.Ping.Inner", wantSource: "backend"},
		{ref: "demo.v1.Ping", wantErr: "several sources"},
		{ref: "demo.v1.Missing", wantErr: "not found"},
		{ref: "nope:demo.v1.Ping", wantErr: "no schema source"},
		{ref: "backend:demo.v1.Region", wantErr: "not found"},
		{ref: "backend:not a name", wantErr: "invalid message name"},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			md, src, err := reg.FindMessage(tt.ref)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FindMessage(%q) error = %v, want it to contain %q", tt.ref, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindMessage(%q): %v", tt.ref, err)
			}
			if src.Name != tt.wantSource {
				t.Fatalf("FindMessage(%q) source = %q, want %q", tt.ref, src.Name, tt.wantSource)
			}
			if got := Ref(src, md); !strings.HasSuffix(tt.ref, string(md.FullName())) || !strings.HasPrefix(got, tt.wantSource+":") {
				t.Fatalf("Ref = %q for %q", got, tt.ref)
			}
		})
	}
}

func TestFindMessageNotFoundIsWrapped(t *testing.T) {
	_, _, err := loadTestRegistry(t).FindMessage("demo.v1.Missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error %v does not wrap ErrNotFound", err)
	}
}

func TestSourceMessages(t *testing.T) {
	got := loadTestRegistry(t).Source("backend").Messages()
	for _, want := range []protoreflect.FullName{"demo.v1.Ping", "demo.v1.Ping.Inner", "google.protobuf.Timestamp"} {
		if !slices.Contains(got, want) {
			t.Errorf("Messages() = %v, missing %s", got, want)
		}
	}
	for _, name := range got {
		if strings.HasSuffix(string(name), "Entry") {
			t.Errorf("Messages() lists the map entry %s", name)
		}
	}
}

func TestLoadDescriptorSet(t *testing.T) {
	files, err := LoadProtoDir(t.Context(), "testdata/backend")
	if err != nil {
		t.Fatal(err)
	}
	path := writeDescriptorSet(t, files)

	reg, err := Load(t.Context(), []SourceConfig{{Name: "set", Type: TypeDescriptorSet, Path: path}})
	if err != nil {
		t.Fatalf("Load descriptor set: %v", err)
	}
	if _, _, err := reg.FindMessage("set:demo.v1.Ping"); err != nil {
		t.Fatalf("FindMessage after round trip: %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		cfgs []SourceConfig
		want string
	}{
		{name: "unknown type", cfgs: []SourceConfig{{Name: "x", Type: "registry", Path: "."}}, want: "unknown type"},
		{name: "empty dir", cfgs: []SourceConfig{{Name: "x", Type: TypeProtoDir, Path: t.TempDir()}}, want: "no .proto files"},
		{name: "missing set", cfgs: []SourceConfig{{Name: "x", Type: TypeDescriptorSet, Path: "nope.binpb"}}, want: "read descriptor set"},
		{
			name: "duplicate name",
			cfgs: []SourceConfig{
				{Name: "x", Type: TypeProtoDir, Path: "testdata/backend"},
				{Name: "x", Type: TypeProtoDir, Path: "testdata/mapsvc"},
			},
			want: "duplicate source name",
		},
		{name: "colon in name", cfgs: []SourceConfig{{Name: "a:b", Type: TypeProtoDir, Path: "testdata/backend"}}, want: "invalid source name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(t.Context(), tt.cfgs)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadDescriptorSetWithoutImports(t *testing.T) {
	files, err := LoadProtoDir(t.Context(), "testdata/backend")
	if err != nil {
		t.Fatal(err)
	}
	fd, err := files.FindFileByPath("demo/v1/demo.proto")
	if err != nil {
		t.Fatal(err)
	}
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{protodesc.ToFileDescriptorProto(fd)}}
	path := filepath.Join(t.TempDir(), "partial.binpb")
	writeSet(t, path, set)

	if _, err := LoadDescriptorSet(t.Context(), path); err == nil || !strings.Contains(err.Error(), "--include_imports") {
		t.Fatalf("error = %v, want a hint about --include_imports", err)
	}
}

func writeDescriptorSet(t *testing.T, files *protoregistry.Files) string {
	t.Helper()
	set := new(descriptorpb.FileDescriptorSet)
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		set.File = append(set.File, protodesc.ToFileDescriptorProto(fd))
		return true
	})
	path := filepath.Join(t.TempDir(), "set.binpb")
	writeSet(t, path, set)
	return path
}

func writeSet(t *testing.T, path string, set *descriptorpb.FileDescriptorSet) {
	t.Helper()
	data, err := proto.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
