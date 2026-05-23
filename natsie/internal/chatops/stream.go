package chatops

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/jsm.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// streamDetail renders a thorough single-stream view: configuration,
// state, replica health. Output is several small sections rather than
// one giant table so chat readers can scan it.
func streamDetail(_ context.Context, ctxName, name string) string {
	nc, err := natsctx.Connect(ctxName)
	if err != nil {
		return fmt.Sprintf("connect %s: %v", ctxName, err)
	}
	defer nc.Close()

	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return fmt.Sprintf("jsm: %v", err)
	}

	s, err := mgr.LoadStream(name)
	if err != nil {
		return fmt.Sprintf("load stream `%s`: %v", name, err)
	}

	info, err := s.Information()
	if err != nil {
		return fmt.Sprintf("info `%s`: %v", name, err)
	}

	state := info.State
	cfg := s.Configuration()

	var b strings.Builder

	fmt.Fprintf(&b, "**`%s` / `%s`**\n\n", ctxName, name)

	b.WriteString("**State**\n\n")
	b.WriteString(mdTable(
		[]string{"Msgs", "Bytes", "Consumers", "First Seq", "Last Seq"},
		[][]string{{
			humanInt(state.Msgs),
			humanBytes(state.Bytes),
			intStr(state.Consumers),
			humanInt(state.FirstSeq),
			humanInt(state.LastSeq),
		}},
	))

	b.WriteString("\n**Configuration**\n\n")
	b.WriteString(mdTable(
		[]string{"Retention", "Storage", "Replicas", "Max Age", "Max Bytes", "Max Msgs"},
		[][]string{{
			fmt.Sprintf("`%s`", cfg.Retention),
			fmt.Sprintf("`%s`", cfg.Storage),
			intStr(cfg.Replicas),
			cfg.MaxAge.String(),
			humanBytesSigned(cfg.MaxBytes),
			humanIntSigned(cfg.MaxMsgs),
		}},
	))

	if len(cfg.Subjects) > 0 {
		fmt.Fprintf(&b, "\n**Subjects** (%d)\n\n", len(cfg.Subjects))

		for i, sub := range cfg.Subjects {
			if i >= 10 {
				fmt.Fprintf(&b, "- …and %d more\n", len(cfg.Subjects)-10)

				break
			}

			fmt.Fprintf(&b, "- `%s`\n", sub)
		}
	}

	if info.Cluster != nil {
		b.WriteString("\n**Cluster**\n\n")

		clusterRows := [][]string{}
		if info.Cluster.Leader != "" {
			clusterRows = append(clusterRows, []string{"leader", info.Cluster.Leader, "—", "—"})
		}

		for _, r := range info.Cluster.Replicas {
			current := "✅"
			if !r.Current {
				current = "❌"
			}

			active := r.Active.Truncate(1).String()
			if r.Active == 0 {
				active = "—"
			}

			clusterRows = append(clusterRows, []string{"replica", r.Name, current, active})
		}

		b.WriteString(mdTable([]string{"Role", "Name", "Current", "Last Active"}, clusterRows))
	}

	return b.String()
}

// humanIntSigned formats an int64 that may be -1 ("unlimited" in
// JetStream's limit fields) as "—" rather than as a misleading
// 18446744073709551615 if we ran it through humanInt.
func humanIntSigned(n int64) string {
	if n <= 0 {
		return "—"
	}

	return humanInt(uint64(n)) //nolint:gosec // n>0 verified above
}

// humanBytesSigned is the byte-cousin of humanIntSigned.
func humanBytesSigned(n int64) string {
	if n <= 0 {
		return "—"
	}

	return humanBytes(uint64(n)) //nolint:gosec // n>0 verified above
}
