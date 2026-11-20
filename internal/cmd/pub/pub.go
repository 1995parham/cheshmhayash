// Package pub implements `cheshmhayash pub`: publish a message, encoding
// JSON into protobuf when the subject has a schema, and optionally wait
// for a reply (request-reply) and print it decoded.
package pub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/cliutil"
	"github.com/1995parham/cheshmhayash/internal/codec"
	"github.com/1995parham/cheshmhayash/internal/render"
	"github.com/1995parham/cheshmhayash/internal/schema"
)

// maxStdin bounds the payload read from stdin.
const maxStdin = 64 << 20

// Command returns the `pub` command.
func Command() *cli.Command {
	flags := append(cliutil.ConnFlags(), cliutil.SchemaFlags()...)
	flags = append(flags,
		&cli.StringFlag{
			Name:    "type",
			Aliases: []string{"t"},
			Usage:   "encode the JSON payload as this message (default: from the subject rules)",
		},
		&cli.BoolFlag{Name: "raw", Usage: "send the payload bytes as they are, without protobuf encoding"},
		&cli.StringSliceFlag{Name: "header", Aliases: []string{"H"}, Usage: "add a header, as Key=Value; repeat for more"},
		&cli.IntFlag{Name: "count", Aliases: []string{"n"}, Usage: "publish the message this many times", Value: 1},
		&cli.BoolFlag{Name: "request", Aliases: []string{"r"}, Usage: "wait for a reply and print it decoded"},
		&cli.StringFlag{Name: "reply-type", Usage: "decode the reply as this message"},
		&cli.DurationFlag{Name: "timeout", Usage: "how long to wait for the server (and for the reply with --request)", Value: 5 * time.Second},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "reply output: pretty, json or raw", Value: string(render.ModePretty)},
		&cli.BoolFlag{Name: "no-color", Usage: "do not color the pretty output", Sources: cli.EnvVars("NO_COLOR")},
	)
	return &cli.Command{
		Name:      "pub",
		Aliases:   []string{"publish"},
		Usage:     "publish a message (JSON in, protobuf on the wire when the subject has a schema)",
		ArgsUsage: "SUBJECT [PAYLOAD|-]",
		Description: "The payload is the second argument, or stdin when it is '-' or missing. When a protobuf\n" +
			"type is known (--type or a subject rule), the payload is protojson and is sent in the\n" +
			"wire form; otherwise, or with --raw, the bytes are sent as they are.",
		Flags:  flags,
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 || len(args) > 2 {
		return errors.New("usage: cheshmhayash pub SUBJECT [PAYLOAD|-]")
	}
	subject := args[0]
	if err := schema.ValidatePattern(subject); err != nil || strings.ContainsAny(subject, "*>") {
		return fmt.Errorf("publish needs a literal subject, got %q", subject)
	}
	header, err := parseHeaders(cmd.StringSlice("header"))
	if err != nil {
		return err
	}
	payload, err := readPayload(args, cmd.Root().Reader)
	if err != nil {
		return err
	}

	settings, err := cliutil.ClientSettings(cmd)
	if err != nil {
		return err
	}
	schemas, err := cliutil.LoadSchemas(ctx, cmd, settings)
	if err != nil {
		return err
	}

	data, kind, err := encode(cmd, schemas, subject, header, payload)
	if err != nil {
		return err
	}

	cluster, err := cliutil.ResolveCluster(cmd, settings)
	if err != nil {
		return err
	}
	nc, err := cliutil.Dial(cluster)
	if err != nil {
		return err
	}
	defer nc.Close()

	msg := &nats.Msg{Subject: subject, Header: header, Data: data}
	if cmd.Bool("request") {
		return request(ctx, cmd, nc, msg, schemas)
	}

	n := max(cmd.Int("count"), 1)
	for range n {
		if err := nc.PublishMsg(msg); err != nil {
			return fmt.Errorf("publish: %w", err)
		}
	}
	// FlushWithContext needs a deadline; the server must confirm the
	// messages within the request timeout.
	flushCtx, cancel := context.WithTimeout(ctx, cmd.Duration("timeout"))
	defer cancel()
	if err := nc.FlushWithContext(flushCtx); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.Root().ErrWriter, "published %d × %d B to %s as %s at %s\n", n, len(data), subject, kind, cluster.Name)
	return nil
}

// encode returns the bytes to send and a word on how they were made.
func encode(cmd *cli.Command, schemas *cliutil.Schemas, subject string, header nats.Header, payload []byte) ([]byte, string, error) {
	if cmd.Bool("raw") {
		return payload, "raw bytes", nil
	}
	ref, origin := schemas.Proto(cmd.String("type"), false).Resolve(codec.Message{Subject: subject, Header: codec.Header(header)})
	if ref == "" {
		return payload, "raw bytes (no protobuf type for this subject)", nil
	}
	data, md, err := codec.Encode(schemas.Registry, ref, payload)
	if err != nil {
		return nil, "", fmt.Errorf("%w (type from %s; use --raw to send the bytes as they are)", err, origin)
	}
	return data, string(md.FullName()) + " (from " + origin + ")", nil
}

func request(ctx context.Context, cmd *cli.Command, nc *nats.Conn, msg *nats.Msg, schemas *cliutil.Schemas) error {
	ctx, cancel := context.WithTimeout(ctx, cmd.Duration("timeout"))
	defer cancel()
	reply, err := nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("request %s: %w", msg.Subject, err)
	}
	mode, err := render.ParseMode(cmd.String("output"))
	if err != nil {
		return err
	}
	decoded := codec.Message{Subject: reply.Subject, Header: codec.Header(reply.Header), Data: reply.Data}
	out := cmd.Root().Writer
	p := &render.Printer{W: out, Mode: mode, Color: !cmd.Bool("no-color") && render.IsTerminal(out), ShowHeaders: true}
	return p.Print(render.Envelope{
		Time:    time.Now(),
		Subject: reply.Subject,
		Header:  decoded.Header,
		Data:    reply.Data,
		Result:  schemas.Chain(cmd.String("reply-type"), false).Decode(decoded),
	})
}

func readPayload(args []string, in io.Reader) ([]byte, error) {
	if len(args) == 2 && args[1] != "-" {
		return []byte(args[1]), nil
	}
	if len(args) == 1 && (in == nil || render.IsTerminalInput(in)) {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(in, maxStdin))
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return data, nil
}

func parseHeaders(kvs []string) (nats.Header, error) {
	if len(kvs) == 0 {
		return nil, nil
	}
	h := nats.Header{}
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("header %q: want Key=Value", kv)
		}
		h.Add(k, v)
	}
	return h, nil
}
