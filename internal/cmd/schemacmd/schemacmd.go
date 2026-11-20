// Package schemacmd implements `cheshmhayash schema`: inspect the loaded
// protobuf schemas and the subject rules, without a NATS connection.
package schemacmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/1995parham/cheshmhayash/internal/cliutil"
	"github.com/1995parham/cheshmhayash/internal/schema"
)

// Command returns the `schema` command group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "schema",
		Usage: "inspect protobuf schemas and subject rules",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list the schema sources",
				Flags: append(cliutil.SchemaFlags(),
					&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "explain every skipped file"},
				),
				Action: list,
			},
			{
				Name:      "messages",
				Usage:     "list message names, optionally only those that contain FILTER",
				ArgsUsage: "[FILTER]",
				Flags: append(cliutil.SchemaFlags(),
					&cli.StringFlag{Name: "source", Usage: "only this source"},
				),
				Action: messages,
			},
			{
				Name:      "show",
				Usage:     "show the fields of a message",
				ArgsUsage: "MESSAGE",
				Flags:     cliutil.SchemaFlags(),
				Action:    show,
			},
			{
				Name:      "resolve",
				Usage:     "show which message each subject decodes as",
				ArgsUsage: "SUBJECT [SUBJECT...]",
				Flags:     cliutil.SchemaFlags(),
				Action:    resolve,
			},
		},
	}
}

func load(ctx context.Context, cmd *cli.Command) (*cliutil.Schemas, error) {
	settings, err := cliutil.ClientSettings(cmd)
	if err != nil {
		return nil, err
	}
	return cliutil.LoadSchemas(ctx, cmd, settings)
}

func list(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	if s.Registry.Len() == 0 {
		_, err := fmt.Fprintln(cmd.Root().ErrWriter, "no schema sources: add [[schemas.sources]] to the settings file or pass --schemas")
		return err
	}
	t := newTable(cmd.Root().Writer)
	t.row("SOURCE", "TYPE", "FILES", "SKIPPED", "MESSAGES", "PATH")
	for i, src := range s.Registry.Sources() {
		c := s.Config.Sources[i]
		t.row(src.Name, c.Type, strconv.Itoa(src.Files.NumFiles()), strconv.Itoa(len(src.Skipped)),
			strconv.Itoa(len(src.Messages())), c.Path)
	}
	if cmd.Bool("verbose") {
		for _, src := range s.Registry.Sources() {
			for _, err := range src.Skipped {
				t.row()
				t.row("skipped in " + src.Name + ": " + err.Error())
			}
		}
	}
	t.row()
	t.row(strconv.Itoa(len(s.Router.Rules())) + " subject rules")
	return t.flush()
}

func messages(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	filter := strings.ToLower(cmd.Args().First())
	only := cmd.String("source")
	if only != "" && s.Registry.Source(only) == nil {
		return fmt.Errorf("no schema source %q", only)
	}
	t := newTable(cmd.Root().Writer)
	for _, src := range s.Registry.Sources() {
		if only != "" && src.Name != only {
			continue
		}
		for _, name := range src.Messages() {
			if filter == "" || strings.Contains(strings.ToLower(string(name)), filter) {
				t.row(src.Name + ":" + string(name))
			}
		}
	}
	return t.flush()
}

func show(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errors.New("give a message, for example: cheshmhayash schema show backend:ride.Accept")
	}
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	md, src, err := s.Registry.FindMessage(ref)
	if err != nil {
		return err
	}
	t := newTable(cmd.Root().Writer)
	t.row(fmt.Sprintf("message %s  (%s)", schema.Ref(src, md), md.ParentFile().Path()))
	fields := md.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		extra := ""
		if oneof := fd.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
			extra = "oneof " + string(oneof.Name())
		}
		if ed := fd.Enum(); ed != nil {
			extra = strings.TrimSpace(extra + " " + enumValues(ed))
		}
		t.row("  "+strconv.Itoa(int(fd.Number())), fieldType(fd), string(fd.Name()), extra)
	}
	return t.flush()
}

func fieldType(fd protoreflect.FieldDescriptor) string {
	if fd.IsMap() {
		return "map<" + fieldType(fd.MapKey()) + ", " + fieldType(fd.MapValue()) + ">"
	}
	t := fd.Kind().String()
	switch {
	case fd.Message() != nil:
		t = string(fd.Message().FullName())
	case fd.Enum() != nil:
		t = string(fd.Enum().FullName())
	}
	if fd.IsList() {
		return "repeated " + t
	}
	return t
}

func enumValues(ed protoreflect.EnumDescriptor) string {
	values := ed.Values()
	parts := make([]string, 0, values.Len())
	for i := range values.Len() {
		v := values.Get(i)
		parts = append(parts, fmt.Sprintf("%s=%d", v.Name(), v.Number()))
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func resolve(ctx context.Context, cmd *cli.Command) error {
	subjects := cmd.Args().Slice()
	if len(subjects) == 0 {
		return errors.New("give at least one subject")
	}
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	t := newTable(cmd.Root().Writer)
	t.row("SUBJECT", "RULE", "MESSAGE", "STATUS")
	for _, subj := range subjects {
		rule, ok := s.Router.Resolve(subj)
		if !ok {
			t.row(subj, "-", "-", "no rule: decoded as JSON, text, protowire or base64")
			continue
		}
		status := "ok"
		if md, src, err := s.Registry.FindMessage(rule.Message); err != nil {
			status = err.Error()
		} else {
			rule.Message = schema.Ref(src, md)
		}
		t.row(subj, rule.Pattern, rule.Message, status)
	}
	return t.flush()
}

// table writes tab-aligned rows and keeps the first write error.
type table struct {
	w   *tabwriter.Writer
	err error
}

func newTable(out io.Writer) *table {
	return &table{w: tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)}
}

func (t *table) row(cells ...string) {
	if t.err == nil {
		_, t.err = io.WriteString(t.w, strings.Join(cells, "\t")+"\n")
	}
}

func (t *table) flush() error {
	if t.err != nil {
		return t.err
	}
	return t.w.Flush()
}
