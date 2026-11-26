package main

import (
	"context"
	"flag"
	"io"

	"github.com/mohammedrefaat/go-anything/internal/mcp"
)

func runMCP(global *GlobalFlags, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)

	allowNetwork := fs.Bool("allow-network", false, "Allow HTTP recording and replay to private networks (including localhost)")
	dir := fs.String("dir", ".", "Workspace root for handoff output")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	server := mcp.NewServer()
	server.AllowNetwork = *allowNetwork
	server.WorkspaceRoot = *dir
	ctx := context.Background()
	return server.Serve(ctx, stdin, stdout)
}
