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
	// ImportPaths are more import roots for proto_dir, for example a copy
	// of googleapis that the files import but the folder does not hold.
	ImportPaths []string
}

// Loaded is what a loader returns: the files, and the files it skipped.
type Loaded struct {
	Files *protoregistry.Files
	// Skipped explains each file that did not load. The other files of
	// the source still work.
	Skipped []error
}

// Loader reads the files of one source.
type Loader func(ctx context.Context, cfg SourceConfig) (Loaded, error)

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
		loaded, err := load(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("schema source %q: %w", c.Name, err)
		}
		if err := reg.Add(c.Name, loaded.Files); err != nil {
			return nil, err
		}
		reg.Source(c.Name).Skipped = loaded.Skipped
	}
	return reg, nil
}

// LoadDescriptorSet reads the serialized FileDescriptorSet at cfg.Path.
func LoadDescriptorSet(_ context.Context, cfg SourceConfig) (Loaded, error) {
	files, err := ReadDescriptorSet(cfg.Path)
	if err != nil {
		return Loaded{}, err
	}
	return Loaded{Files: files}, nil
}

// ReadDescriptorSet reads a serialized FileDescriptorSet from path.
func ReadDescriptorSet(path string) (*protoregistry.Files, error) {
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

// LoadProtoDir compiles every .proto file under cfg.Path, with cfg.Path
// and then cfg.ImportPaths as the import roots. When some files do not
// compile (a missing import, a syntax error), they are skipped and
// reported in Loaded.Skipped; the source fails only when no file
// compiles.
func LoadProtoDir(ctx context.Context, cfg SourceConfig) (Loaded, error) {
	dir := cfg.Path
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
		return Loaded{}, fmt.Errorf("walk %s: %w", dir, err)
	}
	if len(names) == 0 {
		return Loaded{}, fmt.Errorf("no .proto files under %s", dir)
	}
	slices.Sort(names)

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: append([]string{dir}, cfg.ImportPaths...),
		}),
	}

	files := new(protoregistry.Files)
	// The fast path: every file compiles in one pass.
	if compiled, err := compiler.Compile(ctx, names...); err == nil {
		for _, fd := range compiled {
			if err := register(files, fd); err != nil {
				return Loaded{}, err
			}
		}
		return Loaded{Files: files}, nil
	}

	// Some file failed. Compile each file alone and keep the ones that
	// work, so one broken import does not hide the whole repository.
	var skipped []error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return Loaded{}, err
		}
		compiled, err := compiler.Compile(ctx, name)
		if err != nil {
			skipped = append(skipped, err)
			continue
		}
		if err := register(files, compiled[0]); err != nil {
			skipped = append(skipped, err)
		}
	}
	if files.NumFiles() == 0 {
		return Loaded{}, fmt.Errorf("no file under %s compiles: %w", dir, errors.Join(skipped...))
	}
	return Loaded{Files: files, Skipped: skipped}, nil
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
