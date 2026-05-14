package natsx

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ServerEndpoints enumerates the suffixes accepted under
// `$SYS.REQ.SERVER.<id>.<endpoint>` and `$SYS.REQ.SERVER.PING.<endpoint>`.
var ServerEndpoints = []string{
	"VARZ", "CONNZ", "ROUTEZ", "GATEWAYZ", "LEAFZ", "SUBSZ",
	"JSZ", "ACCOUNTZ", "HEALTHZ", "STATSZ",
}

// AccountEndpoints enumerates the suffixes accepted under
// `$SYS.REQ.ACCOUNT.<account>.<endpoint>`.
var AccountEndpoints = []string{"CONNZ", "LEAFZ", "SUBSZ", "JSZ", "INFO"}

func upper(s string) string { return strings.ToUpper(s) }

// IsServerEndpoint reports whether ep (case-insensitive) is a valid
// `$SYS.REQ.SERVER` suffix.
func IsServerEndpoint(ep string) bool { return slices.Contains(ServerEndpoints, upper(ep)) }

// IsAccountEndpoint reports whether ep (case-insensitive) is a valid
// `$SYS.REQ.ACCOUNT` suffix.
func IsAccountEndpoint(ep string) bool { return slices.Contains(AccountEndpoints, upper(ep)) }

// Ping discovers every server in the cluster via $SYS.REQ.SERVER.PING.
func (c *Cluster) Ping() ([]json.RawMessage, error) {
	return c.discover("$SYS.REQ.SERVER.PING", nil, c.discoveryTimeout)
}

// PingEndpoint pings every server for the given monitoring endpoint.
func (c *Cluster) PingEndpoint(endpoint string) ([]json.RawMessage, error) {
	return c.discover("$SYS.REQ.SERVER.PING."+upper(endpoint), nil, c.discoveryTimeout)
}

// ServerEndpoint calls a single server's endpoint targeted by server ID.
func (c *Cluster) ServerEndpoint(id, endpoint string) (json.RawMessage, error) {
	return c.requestJSON(fmt.Sprintf("$SYS.REQ.SERVER.%s.%s", id, upper(endpoint)), nil)
}

// AccountEndpoint queries an account-scoped endpoint across the cluster.
func (c *Cluster) AccountEndpoint(account, endpoint string) ([]json.RawMessage, error) {
	return c.discover(fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.%s", account, upper(endpoint)), nil, c.discoveryTimeout)
}

// Reload triggers a configuration reload on the named server.
func (c *Cluster) Reload(id string) (json.RawMessage, error) {
	return c.requestJSON(fmt.Sprintf("$SYS.REQ.SERVER.%s.RELOAD", id), nil)
}

// LameDuck places the named server into lame-duck mode (graceful drain).
func (c *Cluster) LameDuck(id string) (json.RawMessage, error) {
	return c.requestJSON(fmt.Sprintf("$SYS.REQ.SERVER.%s.LDM", id), nil)
}

// Kick forcibly disconnects the connection with the given CID on the named server.
func (c *Cluster) Kick(id string, cid uint64) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]uint64{"cid": cid})
	if err != nil {
		return nil, fmt.Errorf("encode kick payload: %w", err)
	}
	return c.requestJSON(fmt.Sprintf("$SYS.REQ.SERVER.%s.KICK", id), payload)
}
