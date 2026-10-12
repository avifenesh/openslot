package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/calnode/calnode/internal/inboxworker"
)

func runAgent(args []string) {
	f := flag.NewFlagSet("agent", flag.ExitOnError)
	c := inboxworker.Config{}
	f.StringVar(&c.URL, "url", "http://127.0.0.1:8788", "OpenSlot server URL")
	f.StringVar(&c.Engine, "engine", "codex", "codex, claude, or webhook")
	f.StringVar(&c.Binary, "binary", "", "optional path to agent executable")
	f.StringVar(&c.Model, "model", "", "optional model selected in your existing runtime")
	f.StringVar(&c.WebhookURL, "webhook-url", "", "external agent endpoint (engine webhook)")
	f.BoolVar(&c.Once, "once", false, "process at most one request and exit")
	_ = f.Parse(args)
	c.APIKey = os.Getenv("OPENSLOT_API_KEY")
	c.WebhookToken = os.Getenv("OPENSLOT_WEBHOOK_TOKEN")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if e := inboxworker.Run(ctx, c); e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
