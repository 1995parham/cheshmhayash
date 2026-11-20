// Command cheshmhayash is a toolbox for NATS administration. One binary
// holds every component: the HTTP dashboard (`serve`) and the MCP server
// (`mcp`). The command tree lives in internal/cmd.
package main

import "github.com/1995parham/cheshmhayash/internal/cmd"

func main() {
	cmd.Execute()
}
