package schema

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// SourceType names a way to load a schema source.
type SourceType string

const (
	// TypeDescriptorSet reads a serialized google.protobuf.FileDescriptorSet,
	// as `protoc --include_imports --descriptor_set_out` or `buf build -o`
	// write it. Every import must be in the set.
	TypeDescriptorSet SourceType = "descriptor_set"
	// TypeProtoDir compiles every .proto file under a folder at start. The
	// folder is the import root, the same as `protoc -I <dir>`. The
	// google/protobuf well-known types are always available.
	TypeProtoDir SourceType = "proto_dir"
)

// SourceConfig describes one source to load.
type SourceConfig struct {
	Name string
	Type SourceType
	Path string
}

// Loader reads the files of one source from path.
type Loader func(ctx context.Context, path string) (*protoregistry.Files, error)

// Loaders maps each source type to its loader. A build that needs another
// kind of source (a schema registry, gRPC reflection) adds an entry here.
//
//nolint:gochecknoglobals // the extension point for source types; written only at init
var Loaders = map[SourceType]Loader{
	TypeDescriptorSet: LoadDescriptorSet,
	TypeProtoDir:      LoadProtoDir,
}

// Load builds a registry from cfgs, in order.
func Load(ctx context.Context, cfgs []SourceConfig) (*Registry, error) {
	reg := NewRegistry()
	for _, c := range cfgs {
		load, ok := Loaders[c.Type]
		if !ok {
			return nil, fmt.Errorf("schema source %q: unknown type %q (known: %s, %s)",
				c.Name, c.Type, TypeDescriptorSet, TypeProtoDir)
		}
		files, err := load(ctx, c.Path)
		if err != nil {
			return nil, fmt.Errorf("schema source %q: %w", c.Name, err)
		}
		if err := reg.Add(c.Name, files); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// LoadDescriptorSet reads a serialized FileDescriptorSet from path.
func LoadDescriptorSet(_ context.Context, path string) (*protoregistry.Files, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the operator names the descriptor set in config; reading it is the point
	if err != nil {
		return nil, fmt.Errorf("read descriptor set: %w", err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("parse descriptor set %s: %w", path, err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("link descriptor set %s (was it built with --include_imports?): %w", path, err)
	}
	return files, nil
}

// LoadProtoDir compiles every .proto file under dir, with dir as the
// import root.
func LoadProtoDir(ctx context.Context, dir string) (*protoregistry.Files, error) {
	var names []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".proto" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no .proto files under %s", dir)
	}
	slices.Sort(names)

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{dir},
		}),
	}
	compiled, err := compiler.Compile(ctx, names...)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", dir, err)
	}

	files := new(protoregistry.Files)
	for _, fd := range compiled {
		if err := register(files, fd); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// register adds fd and, first, every file it imports that files does not
// hold yet. The well-known types arrive this way.
func register(files *protoregistry.Files, fd protoreflect.FileDescriptor) error {
	if _, err := files.FindFileByPath(fd.Path()); err == nil {
		return nil
	} else if !errors.Is(err, protoregistry.NotFound) {
		return fmt.Errorf("look up %s: %w", fd.Path(), err)
	}
	imports := fd.Imports()
	for i := range imports.Len() {
		if err := register(files, imports.Get(i).FileDescriptor); err != nil {
			return err
		}
	}
	if err := files.RegisterFile(fd); err != nil {
		return fmt.Errorf("register %s: %w", fd.Path(), err)
	}
	return nil
}
