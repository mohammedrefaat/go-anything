package main

import (
	"context"
	"flag"
	"io"

	"github.com/mohammedrefat/go-anything/internal/mcp"
)

func runMCP(global *GlobalFlags, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	server := mcp.NewServer()
	ctx := context.Background()
	return server.Serve(ctx, stdin, stdout)
}
