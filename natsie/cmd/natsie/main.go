package main

import (
	"fmt"
	"os"

	"github.com/1995parham/natsie/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
