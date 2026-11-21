// Package health finds JetStream assets that misbehave in a JSZ overview:
// stale consumers, consumers that cannot take more work, streams that can
// grow without bound or are near a limit, Raft groups without a leader or
// with replicas that are offline or behind, and ghost peers.
//
// It reuses the rules of internal/ops/scanner (ClassifyConsumer, IsUnlimited,
// AggregatePeers, PlacementSkew), so the dashboard, the MCP server and the
// operations commands agree on what "bad" means. It does no I/O: the input is
// the overview that natsx.OverviewCache already holds.
package health

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/jsm.go/api"

	"github.com/1995parham/cheshmhayash/internal/ops/protect"
	"github.com/1995parham/cheshmhayash/internal/ops/scanner"
)

// Severity orders findings by how soon an operator must act.
type Severity string

// The severities, most urgent first.
const (
	// Critical means traffic is affected now or one more failure affects it.
	Critical Severity = "critical"
	// Warning means a problem that grows if nobody acts.
	Warning Severity = "warning"
	// Info means a fact worth knowing that can be on purpose.
	Info Severity = "info"
)

func (s Severity) rank() int {
	switch s {
	case Critical:
		return 0
	case Warning:
		return 1
	case Info:
		return 2 //nolint:mnd // the ranks are the order of the constants
	default:
		return 3 //nolint:mnd // unknown severities sort last
	}
}

// Kind names one rule.
type Kind string

// The rules.
const (
	// KindStaleConsumer is a consumer with a backlog that nobody reads.
	KindStaleConsumer Kind = "stale_consumer"
	// KindAckPendingFull is a consumer at max_ack_pending: the server sends it nothing more.
	KindAckPendingFull Kind = "ack_pending_full"
	// KindNoLeader is a stream or consumer Raft group without a leader.
	KindNoLeader Kind = "no_leader"
	// KindReplicaOffline is a Raft group with an offline replica.
	KindReplicaOffline Kind = "replica_offline"
	// KindReplicaLagging is a Raft group with a replica that is not current.
	KindReplicaLagging Kind = "replica_lagging"
	// KindUnlimitedStream is a limits stream with no max_age, max_bytes or max_msgs.
	KindUnlimitedStream Kind = "unlimited_stream"
	// KindNearLimit is a stream near max_bytes or max_msgs.
	KindNearLimit Kind = "near_limit"
	// KindUnderReplicated is a stream with one replica.
	KindUnderReplicated Kind = "under_replicated"
	// KindGhostPeer is a server offline in every Raft group that names it.
	KindGhostPeer Kind = "ghost_peer"
	// KindLaggingPeer is a server current in no Raft group that names it.
	KindLaggingPeer Kind = "lagging_peer"
)

// Finding is one problem on one asset.
type Finding struct {
	Kind     Kind     `json:"kind"`
	Severity Severity `json:"severity"`
	Account  string   `json:"account,omitempty"`
	Stream   string   `json:"stream,omitempty"`
	Consumer string   `json:"consumer,omitempty"`
	Peer     string   `json:"peer,omitempty"`
	Message  string   `json:"message"`
	// Pending and Idle are set on consumer findings.
	Pending int64         `json:"pending,omitempty"`
	Idle    time.Duration `json:"idle,omitempty"`
	// RenamedTo names the active consumer that took over a stale one's filter.
	RenamedTo string `json:"renamed_to,omitempty"`
	// Managed says what owns the consumer (NACK, Terraform, GitOps); such a
	// consumer is not cheshmhayash's to delete.
	Managed string `json:"managed,omitempty"`
}

// Summary counts the findings and the assets that the analysis read.
type Summary struct {
	Critical  int `json:"critical"`
	Warning   int `json:"warning"`
	Info      int `json:"info"`
	Streams   int `json:"streams"`
	Consumers int `json:"consumers"`
}

// Report is the result of Analyze.
type Report struct {
	Summary   Summary              `json:"summary"`
	Findings  []Finding            `json:"findings"`
	Peers     []scanner.PeerRow    `json:"peers"`
	Placement []scanner.ServerLoad `json:"placement"`
}

// Options are the thresholds of the rules.
type Options struct {
	// MinIdle is how long a consumer goes without an ack before it can be stale.
	MinIdle time.Duration
	// MinPending is the smallest backlog that makes an idle consumer stale.
	MinPending int64
	// NearLimit is the fill ratio (0..1] of max_bytes or max_msgs that is "near".
	NearLimit float64
	// Protect marks consumers that something else owns. Nil marks none.
	Protect *protect.Protector
}

// Default thresholds, the same as the operations commands use.
const (
	DefaultMinIdle    = 24 * time.Hour
	DefaultMinPending = 1
	DefaultNearLimit  = 0.9
)

// DefaultOptions returns the default thresholds and honours the default
// protect metadata key.
func DefaultOptions() Options {
	p, _ := protect.New(protect.Config{}) // an empty rule set cannot fail validation

	return Options{MinIdle: DefaultMinIdle, MinPending: DefaultMinPending, NearLimit: DefaultNearLimit, Protect: p}
}

// jszReply is one server's answer to $SYS.REQ.SERVER.PING.JSZ, with only
// the fields the rules read.
type jszReply struct {
	Server struct {
		Name string `json:"name"`
	} `json:"server"`
	Data struct {
		Accounts []struct {
			Name    string         `json:"name"`
			Streams []streamDetail `json:"stream_detail"`
		} `json:"account_details"`
	} `json:"data"`
}

type streamDetail struct {
	Name      string              `json:"name"`
	Cluster   *api.ClusterInfo    `json:"cluster,omitempty"`
	Config    *api.StreamConfig   `json:"config,omitempty"`
	State     api.StreamState     `json:"state"`
	Consumers []*api.ConsumerInfo `json:"consumer_detail,omitempty"`
}

type streamKey struct{ account, stream string }

type consumerKey struct{ account, stream, consumer string }

// snapshot is the overview folded to one copy of each asset: the copy from
// the server that leads it, else the first copy seen.
type snapshot struct {
	streams      map[streamKey]streamDetail
	consumers    map[consumerKey]*api.ConsumerInfo
	streamOrder  []streamKey
	consumerKeys []consumerKey
}

// leads reports whether server leads the group, or whether nobody does and
// the caller has no copy yet.
func leads(ci *api.ClusterInfo, server string) bool {
	return ci != nil && ci.Leader != "" && ci.Leader == server
}

func fold(overview []byte) (snapshot, error) {
	var replies []jszReply
	if err := json.Unmarshal(overview, &replies); err != nil {
		return snapshot{}, fmt.Errorf("decode overview: %w", err)
	}

	s := snapshot{
		streams:      map[streamKey]streamDetail{},
		consumers:    map[consumerKey]*api.ConsumerInfo{},
		streamOrder:  nil,
		consumerKeys: nil,
	}

	for _, r := range replies {
		for _, a := range r.Data.Accounts {
			for _, st := range a.Streams {
				sk := streamKey{a.Name, st.Name}
				if _, seen := s.streams[sk]; !seen {
					s.streamOrder = append(s.streamOrder, sk)
					s.streams[sk] = st
				} else if leads(st.Cluster, r.Server.Name) {
					s.streams[sk] = st
				}

				for _, c := range st.Consumers {
					if c == nil {
						continue
					}

					ck := consumerKey{a.Name, st.Name, c.Name}
					if _, seen := s.consumers[ck]; !seen {
						s.consumerKeys = append(s.consumerKeys, ck)
						s.consumers[ck] = c
					} else if leads(c.Cluster, r.Server.Name) {
						s.consumers[ck] = c
					}
				}
			}
		}
	}

	return s, nil
}

// Analyze runs every rule over overview, the JSON array of JSZ replies that
// natsx.Cluster.JSMOverview returns, as of now.
func Analyze(overview []byte, now time.Time, opts Options) (Report, error) {
	snap, err := fold(overview)
	if err != nil {
		return Report{}, err
	}

	var (
		findings []Finding
		groups   []scanner.RaftGroup
		reports  []scanner.StreamReportRow
	)

	for _, sk := range snap.streamOrder {
		st := snap.streams[sk]
		findings = append(findings, streamFindings(sk, st, opts)...)
		findings = append(findings, raftFindings(sk.account, sk.stream, "", st.Cluster)...)

		row := scanner.StreamReportRow{Stream: st.Name, Messages: st.State.Msgs, Bytes: st.State.Bytes}
		if st.Config != nil {
			row.Replicas = st.Config.Replicas
		}

		if st.Cluster != nil {
			groups = append(groups, raftGroup(st.Name, st.Cluster))
			row.Leader = st.Cluster.Leader
			row.Peers = scanner.PlacementPeers(*st.Cluster)
		}

		reports = append(reports, row)
	}

	rows := make([]scanner.Row, 0, len(snap.consumerKeys))

	for _, ck := range snap.consumerKeys {
		c := snap.consumers[ck]
		findings = append(findings, raftFindings(ck.account, ck.stream, ck.consumer, c.Cluster)...)
		findings = append(findings, ackPendingFindings(ck, c)...)

		if c.Cluster != nil {
			groups = append(groups, raftGroup(ck.stream+"/"+ck.consumer, c.Cluster))
		}

		r := scanner.ClassifyConsumer(c, now, opts.MinIdle)
		r.Cluster = ck.account // carries the account through DetectRenames
		r.Stream = ck.stream
		r.Consumer = ck.consumer
		r.Managed = opts.Protect.Reason(ck.stream, ck.consumer, c.Config.Metadata)
		rows = append(rows, r)
	}

	findings = append(findings, staleFindings(rows, opts)...)

	peers := scanner.AggregatePeers("", groups)
	findings = append(findings, peerFindings(peers)...)

	sortFindings(findings)

	rep := Report{
		Summary:   Summary{Streams: len(snap.streamOrder), Consumers: len(snap.consumerKeys)},
		Findings:  findings,
		Peers:     peers,
		Placement: scanner.PlacementSkew(reports),
	}

	for _, f := range findings {
		switch f.Severity {
		case Critical:
			rep.Summary.Critical++
		case Warning:
			rep.Summary.Warning++
		case Info:
			rep.Summary.Info++
		}
	}

	if rep.Findings == nil {
		rep.Findings = []Finding{}
	}

	return rep, nil
}

func raftGroup(name string, ci *api.ClusterInfo) scanner.RaftGroup {
	replicas := make([]api.PeerInfo, 0, len(ci.Replicas))
	for _, p := range ci.Replicas {
		if p != nil {
			replicas = append(replicas, *p)
		}
	}

	return scanner.RaftGroup{Name: name, Leader: ci.Leader, Replicas: replicas}
}

func streamFindings(sk streamKey, st streamDetail, opts Options) []Finding {
	if st.Config == nil {
		return nil
	}

	cfg := *st.Config
	base := Finding{Account: sk.account, Stream: sk.stream}

	var out []Finding

	if scanner.IsUnlimited(cfg) {
		f := base
		f.Kind, f.Severity = KindUnlimitedStream, Warning
		f.Message = fmt.Sprintf("limits retention with no max_age, max_bytes or max_msgs; it holds %d messages, %s",
			st.State.Msgs, bytesText(st.State.Bytes))
		out = append(out, f)
	}

	if f, ok := nearLimit(base, cfg, st.State, opts.NearLimit); ok {
		out = append(out, f)
	}

	if cfg.Replicas < 2 { //nolint:mnd // two replicas is the least redundancy
		f := base
		f.Kind, f.Severity = KindUnderReplicated, Info
		f.Message = "one replica: the stream stops when its server stops"
		out = append(out, f)
	}

	return out
}

func nearLimit(base Finding, cfg api.StreamConfig, state api.StreamState, ratio float64) (Finding, bool) {
	if ratio <= 0 {
		return Finding{}, false
	}

	var what string

	switch {
	case cfg.MaxBytes > 0 && float64(state.Bytes) >= ratio*float64(cfg.MaxBytes):
		what = fmt.Sprintf("%s of max_bytes %s", bytesText(state.Bytes), bytesText(uint64(cfg.MaxBytes)))
	case cfg.MaxMsgs > 0 && float64(state.Msgs) >= ratio*float64(cfg.MaxMsgs):
		what = fmt.Sprintf("%d of max_msgs %d", state.Msgs, cfg.MaxMsgs)
	default:
		return Finding{}, false
	}

	f := base
	f.Kind = KindNearLimit

	if cfg.Discard == api.DiscardNew {
		f.Severity = Critical
		f.Message = what + "; discard is new, so publishes fail at the limit"
	} else {
		f.Severity = Info
		f.Message = what + "; discard is old, so the oldest messages go first"
	}

	return f, true
}

func raftFindings(account, stream, consumer string, ci *api.ClusterInfo) []Finding {
	// A group with one member (R1, or a server without clustering) has no
	// replicas to judge, and JSZ reports it only while its server runs.
	if ci == nil || len(ci.Replicas) == 0 {
		return nil
	}

	base := Finding{Account: account, Stream: stream, Consumer: consumer}

	if ci.Leader == "" {
		f := base
		f.Kind, f.Severity = KindNoLeader, Critical
		f.Message = "the Raft group has no leader: reads and writes stop"

		return []Finding{f}
	}

	var offline, lagging []string

	for _, p := range ci.Replicas {
		switch {
		case p == nil || p.Name == ci.Leader:
		case p.Offline:
			offline = append(offline, p.Name)
		case !p.Current:
			lagging = append(lagging, fmt.Sprintf("%s (%d behind)", p.Name, p.Lag))
		}
	}

	var out []Finding

	if len(offline) > 0 {
		f := base
		f.Kind, f.Severity = KindReplicaOffline, Warning
		f.Message = "offline replicas: " + strings.Join(offline, ", ")
		out = append(out, f)
	}

	if len(lagging) > 0 {
		f := base
		f.Kind, f.Severity = KindReplicaLagging, Warning
		f.Message = "replicas not current: " + strings.Join(lagging, ", ")
		out = append(out, f)
	}

	return out
}

func ackPendingFindings(ck consumerKey, c *api.ConsumerInfo) []Finding {
	limit := c.Config.MaxAckPending
	if limit <= 0 || c.NumAckPending < limit {
		return nil
	}

	return []Finding{{
		Kind:     KindAckPendingFull,
		Severity: Critical,
		Account:  ck.account,
		Stream:   ck.stream,
		Consumer: ck.consumer,
		Message: fmt.Sprintf("%d messages wait for an ack, which is max_ack_pending: the server sends no more until the client acks",
			c.NumAckPending),
		Pending: int64(c.NumPending), //nolint:gosec // counts are bounded by stream depth
	}}
}

func staleFindings(rows []scanner.Row, opts Options) []Finding {
	scanner.DetectRenames(rows)

	var out []Finding

	for _, r := range rows {
		if r.Status != scanner.StatusStale || r.NumPending < opts.MinPending {
			continue
		}

		msg := fmt.Sprintf("%d messages wait and no ack for %s", r.NumPending, idleText(r.Idle))
		if r.RenamedTo != "" {
			msg += "; consumer " + r.RenamedTo + " reads the same filter, so this is likely its old name"
		}

		out = append(out, Finding{
			Kind:      KindStaleConsumer,
			Severity:  Warning,
			Account:   r.Cluster,
			Stream:    r.Stream,
			Consumer:  r.Consumer,
			Message:   msg,
			Pending:   r.NumPending,
			Idle:      r.Idle,
			RenamedTo: r.RenamedTo,
			Managed:   r.Managed,
		})
	}

	return out
}

func peerFindings(peers []scanner.PeerRow) []Finding {
	var out []Finding

	for _, p := range peers {
		switch p.Health {
		case scanner.PeerGhost:
			out = append(out, Finding{
				Kind: KindGhostPeer, Severity: Critical, Peer: p.Peer,
				Message: fmt.Sprintf("offline in all %d Raft groups that name it and leads none: remove the peer if the server is gone", p.Groups),
			})
		case scanner.PeerLagging:
			out = append(out, Finding{
				Kind: KindLaggingPeer, Severity: Warning, Peer: p.Peer,
				Message: fmt.Sprintf("current in none of the %d Raft groups that name it", p.Groups),
			})
		case scanner.PeerOK:
		}
	}

	return out
}

func sortFindings(fs []Finding) {
	slices.SortStableFunc(fs, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.Severity.rank(), b.Severity.rank()),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Account, b.Account),
			cmp.Compare(a.Stream, b.Stream),
			cmp.Compare(a.Consumer, b.Consumer),
			cmp.Compare(a.Peer, b.Peer),
		)
	})
}

func bytesText(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// idleText rounds an idle time to what a reader needs: seconds below a
// minute, minutes below a day, hours after.
func idleText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < 24*time.Hour:
		return d.Round(time.Minute).String()
	default:
		return d.Round(time.Hour).String()
	}
}
