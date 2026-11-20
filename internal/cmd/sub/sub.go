// Package sub implements `cheshmhayash sub`: subscribe to subjects on core
// NATS and print each message decoded. It never creates a JetStream
// consumer, so it is safe to run against production streams.
package sub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/cliutil"
	"github.com/1995parham/cheshmhayash/internal/codec"
	"github.com/1995parham/cheshmhayash/internal/render"
	"github.com/1995parham/cheshmhayash/internal/schema"
)

// pendingMessages is how many messages may wait for the printer before
// the client drops new ones, as NATS does for any slow consumer.
const pendingMessages = 65536

// Command returns the `sub` command.
func Command() *cli.Command {
	flags := append(cliutil.ConnFlags(), cliutil.SchemaFlags()...)
	flags = append(flags,
		&cli.StringFlag{
			Name:    "type",
			Aliases: []string{"t"},
			Usage:   "decode every payload as this message (\"source:full.Name\" or \"full.Name\")",
		},
		&cli.StringFlag{Name: "queue", Aliases: []string{"q"}, Usage: "join this queue group (share messages with other members)"},
		&cli.IntFlag{Name: "count", Aliases: []string{"n"}, Usage: "stop after this many messages (0: no limit)"},
		&cli.DurationFlag{Name: "timeout", Usage: "stop after this long (0: no limit)"},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "pretty, json (one object per line) or raw", Value: string(render.ModePretty)},
		&cli.BoolFlag{Name: "headers", Aliases: []string{"H"}, Usage: "show message headers (pretty output)"},
		&cli.BoolFlag{Name: "emit-defaults", Usage: "show protobuf fields that hold their zero value"},
		&cli.BoolFlag{Name: "no-color", Usage: "do not color the pretty output", Sources: cli.EnvVars("NO_COLOR")},
	)
	return &cli.Command{
		Name:      "sub",
		Aliases:   []string{"subscribe"},
		Usage:     "subscribe on core NATS and print messages decoded (protobuf, JSON, text)",
		ArgsUsage: "SUBJECT [SUBJECT...]",
		Description: "Subjects may hold the wildcards * and >. A protobuf type comes from --type, then the\n" +
			"Proto-Type header, then the subject rules of the schemas; without one the payload is shown\n" +
			"as JSON, text, a schemaless protobuf dump, or base64. Core NATS only: no JetStream\n" +
			"consumer is created, and live messages of a stream's subjects arrive all the same.",
		Flags:  flags,
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	subjects := cmd.Args().Slice()
	if len(subjects) == 0 {
		return errors.New("give at least one subject, for example: cheshmhayash sub 'ride.>'")
	}
	for _, s := range subjects {
		if err := schema.ValidatePattern(s); err != nil {
			return fmt.Errorf("subject %q: %w", s, err)
		}
	}
	mode, err := render.ParseMode(cmd.String("output"))
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
	if t := cmd.String("type"); t != "" {
		if _, _, err := schemas.Registry.FindMessage(t); err != nil {
			return fmt.Errorf("--type: %w", err)
		}
	}
	chain := schemas.Chain(cmd.String("type"), cmd.Bool("emit-defaults"))

	cluster, err := cliutil.ResolveCluster(cmd, settings)
	if err != nil {
		return err
	}
	nc, err := cliutil.Dial(cluster)
	if err != nil {
		return err
	}
	defer nc.Close()

	msgs := make(chan *nats.Msg, pendingMessages)
	subs := make([]*nats.Subscription, 0, len(subjects))
	for _, s := range subjects {
		sub, err := nc.ChanQueueSubscribe(s, cmd.String("queue"), msgs)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", s, err)
		}
		subs = append(subs, sub)
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	slog.Debug("subscribed", "cluster", cluster.Name, "subjects", subjects, "sources", schemas.Registry.Len())
	out, errOut := cmd.Root().Writer, cmd.Root().ErrWriter
	_, _ = fmt.Fprintf(errOut, "listening on %v at %s (%s)\n", subjects, cluster.Name, nc.ConnectedUrlRedacted())

	printer := &render.Printer{
		W:           out,
		Mode:        mode,
		Color:       !cmd.Bool("no-color") && render.IsTerminal(out),
		ShowHeaders: cmd.Bool("headers"),
	}

	if d := cmd.Duration("timeout"); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	received, err := consume(ctx, msgs, chain, printer, cmd.Int("count"))
	dropped := 0
	for _, sub := range subs {
		if n, derr := sub.Dropped(); derr == nil {
			dropped += n
		}
		_ = sub.Unsubscribe()
	}
	summary := fmt.Sprintf("%d messages", received)
	if dropped > 0 {
		summary += fmt.Sprintf(", %d dropped because the output was too slow", dropped)
	}
	_, _ = fmt.Fprintln(errOut, summary)
	return err
}

func consume(ctx context.Context, msgs <-chan *nats.Msg, chain codec.Chain, p *render.Printer, limit int) (int, error) {
	received := 0
	for limit <= 0 || received < limit {
		select {
		case <-ctx.Done():
			return received, nil
		case m := <-msgs:
			received++
			msg := codec.Message{Subject: m.Subject, Header: codec.Header(m.Header), Data: m.Data}
			if err := p.Print(render.Envelope{
				Time:    time.Now(),
				Subject: m.Subject,
				Reply:   m.Reply,
				Header:  msg.Header,
				Data:    m.Data,
				Result:  chain.Decode(msg),
			}); err != nil {
				return received, fmt.Errorf("print: %w", err)
			}
		}
	}
	return received, nil
}
