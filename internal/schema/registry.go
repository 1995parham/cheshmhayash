// Package schema holds the protobuf schemas that cheshmhayash decodes NATS
// payloads with. Schemas are loaded at run time from named sources (a
// descriptor set or a folder of .proto files), so private schemas never
// have to be compiled into the binary.
//
// Each source keeps its own registry. Two sources may define the same full
// message name (two repositories that both declare `package ride;`), so a
// reference can name its source: "backend:ride.Accept".
package schema

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Source is one named set of protobuf file descriptors.
type Source struct {
	// Name is the prefix a message reference uses: "<name>:<full.Name>".
	Name string
	// Files holds every file of the source, its imports included.
	Files *protoregistry.Files
	// Types resolves messages and extensions of the source for protojson,
	// for example the payload of a google.protobuf.Any.
	Types *dynamicpb.Types
}

// Messages returns the full names of every message the source defines,
// nested messages included, in sorted order.
func (s *Source) Messages() []protoreflect.FullName {
	var names []protoreflect.FullName
	s.Files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		names = appendMessages(names, fd.Messages())
		return true
	})
	slices.Sort(names)
	return names
}

func appendMessages(names []protoreflect.FullName, mds protoreflect.MessageDescriptors) []protoreflect.FullName {
	for i := range mds.Len() {
		md := mds.Get(i)
		if md.IsMapEntry() {
			continue
		}
		names = append(names, md.FullName())
		names = appendMessages(names, md.Messages())
	}
	return names
}

// Registry is the ordered set of loaded sources.
type Registry struct {
	sources []*Source
}

// ErrNotFound reports a message reference that no source defines.
var ErrNotFound = errors.New("message not found")

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Add registers files under name. Names must be unique and must not
// contain ':', the separator of a message reference.
func (r *Registry) Add(name string, files *protoregistry.Files) error {
	if name == "" || strings.Contains(name, ":") {
		return fmt.Errorf("invalid source name %q: it must be non-empty and must not contain ':'", name)
	}
	if r.Source(name) != nil {
		return fmt.Errorf("duplicate source name %q", name)
	}
	r.sources = append(r.sources, &Source{
		Name:  name,
		Files: files,
		Types: dynamicpb.NewTypes(files),
	})
	return nil
}

// Sources returns the sources in the order they were added.
func (r *Registry) Sources() []*Source {
	return slices.Clone(r.sources)
}

// Source returns the source called name, or nil.
func (r *Registry) Source(name string) *Source {
	for _, s := range r.sources {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Len returns the number of sources.
func (r *Registry) Len() int {
	return len(r.sources)
}

// FindMessage resolves a reference of the form "source:full.Name" or
// "full.Name". A bare name must be defined by exactly one source.
func (r *Registry) FindMessage(ref string) (protoreflect.MessageDescriptor, *Source, error) {
	sourceName, fullName, qualified := strings.Cut(ref, ":")
	if !qualified {
		fullName, sourceName = sourceName, ""
	}
	name := protoreflect.FullName(fullName)
	if !name.IsValid() {
		return nil, nil, fmt.Errorf("invalid message name %q", fullName)
	}

	if qualified {
		src := r.Source(sourceName)
		if src == nil {
			return nil, nil, fmt.Errorf("%w: no schema source %q", ErrNotFound, sourceName)
		}
		md, err := findIn(src, name)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s in source %q", ErrNotFound, name, sourceName)
		}
		return md, src, nil
	}

	var (
		found   protoreflect.MessageDescriptor
		foundIn *Source
		owners  []string
	)
	for _, src := range r.sources {
		md, err := findIn(src, name)
		if err != nil {
			continue
		}
		if found == nil {
			found, foundIn = md, src
		}
		owners = append(owners, src.Name)
	}
	switch len(owners) {
	case 0:
		return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	case 1:
		return found, foundIn, nil
	default:
		return nil, nil, fmt.Errorf("message %s is defined by several sources (%s); qualify it as <source>:%s",
			name, strings.Join(owners, ", "), name)
	}
}

func findIn(src *Source, name protoreflect.FullName) (protoreflect.MessageDescriptor, error) {
	d, err := src.Files.FindDescriptorByName(name)
	if err != nil {
		return nil, err
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is a %T, not a message", name, d)
	}
	return md, nil
}

// Ref returns the qualified reference of md in src: "source:full.Name".
func Ref(src *Source, md protoreflect.MessageDescriptor) string {
	return src.Name + ":" + string(md.FullName())
}
