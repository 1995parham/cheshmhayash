package config

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Schemas configures the protobuf schemas that payloads decode with. The
// same shape is the [schemas] table of settings.toml and the top level of
// a standalone schemas file (see LoadSchemas), which a private repository
// can ship next to its descriptor sets.
type Schemas struct {
	// TypeHeader names the message header that carries a payload's type.
	// Empty means the default ("Proto-Type"); "-" turns the lookup off.
	TypeHeader string         `json:"type_header,omitempty" koanf:"type_header"`
	Sources    []SchemaSource `json:"sources,omitempty"     koanf:"sources"`
	Subjects   []SubjectRule  `json:"subjects,omitempty"    koanf:"subjects"`
}

// SchemaSource is one named set of protobuf files.
type SchemaSource struct {
	// Name prefixes message references: "<name>:<full.Name>".
	Name string `json:"name" koanf:"name"`
	// Type is "descriptor_set" or "proto_dir".
	Type string `json:"type" koanf:"type"`
	// Path is the descriptor set file or the .proto import root. A
	// relative path resolves against the folder of the file it is in.
	Path string `json:"path" koanf:"path"`
	// ImportPaths are more import roots for proto_dir (for example a copy
	// of googleapis), resolved like Path.
	ImportPaths []string `json:"import_paths,omitempty" koanf:"import_paths"`
}

// SubjectRule maps the subjects that match a NATS pattern to a message.
type SubjectRule struct {
	Match   string `json:"match"   koanf:"match"`
	Message string `json:"message" koanf:"message"`
}

// IsZero reports whether no schema is configured.
func (s Schemas) IsZero() bool {
	return s.TypeHeader == "" && len(s.Sources) == 0 && len(s.Subjects) == 0
}

// Merge appends other's sources and rules after s's. A non-empty
// TypeHeader in other wins.
func (s Schemas) Merge(other Schemas) Schemas {
	out := Schemas{
		TypeHeader: s.TypeHeader,
		Sources:    append(append([]SchemaSource(nil), s.Sources...), other.Sources...),
		Subjects:   append(append([]SubjectRule(nil), s.Subjects...), other.Subjects...),
	}
	if other.TypeHeader != "" {
		out.TypeHeader = other.TypeHeader
	}
	return out
}

// LoadSchemas reads a standalone schemas file: the Schemas shape at the
// top level of a TOML file. Relative paths resolve against its folder.
func LoadSchemas(path string) (Schemas, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return Schemas{}, fmt.Errorf("load schemas file %s: %w", path, err)
	}
	var s Schemas
	if err := k.Unmarshal("", &s); err != nil {
		return Schemas{}, fmt.Errorf("parse schemas file %s: %w", path, err)
	}
	resolveSchemaPaths(&s, filepath.Dir(path))
	if err := s.validate(); err != nil {
		return Schemas{}, fmt.Errorf("schemas file %s: %w", path, err)
	}
	return s, nil
}

func resolveSchemaPaths(s *Schemas, base string) {
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	for i := range s.Sources {
		s.Sources[i].Path = abs(s.Sources[i].Path)
		for j, p := range s.Sources[i].ImportPaths {
			s.Sources[i].ImportPaths[j] = abs(p)
		}
	}
}

func (s Schemas) validate() error {
	var errs []error
	for i, src := range s.Sources {
		if src.Name == "" || src.Type == "" || src.Path == "" {
			errs = append(errs, fmt.Errorf("sources[%d]: name, type and path are required", i))
		}
	}
	for i, r := range s.Subjects {
		if r.Match == "" || r.Message == "" {
			errs = append(errs, fmt.Errorf("subjects[%d]: match and message are required", i))
		}
	}
	return errors.Join(errs...)
}
