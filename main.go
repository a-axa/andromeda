// Command andromeda ranks this machine's network interfaces by measured
// latency and packet loss, live in the terminal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"andromeda/internal/iface"
	"andromeda/internal/probe"
	"andromeda/internal/score"
	"andromeda/ui"
)

const (
	defaultTarget   = "8.8.8.8"
	defaultInterval = 2 * time.Second
)

func main() {
	err := run(os.Args[1:])
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		// -h already printed usage, so exiting zero is the friendly result.
	default:
		fmt.Fprintln(os.Stderr, "andromeda:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	if err := probe.CheckRawICMP(); err != nil {
		return fmt.Errorf("%w\n%s", err, permissionHint())
	}
	return ui.Run(ui.Options{
		Target:   cfg.target.String(),
		Interval: cfg.interval,
		Source:   source(probe.Config{Target: cfg.target}),
	})
}

type config struct {
	target   net.IP
	interval time.Duration
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("andromeda", flag.ContinueOnError)
	target := fs.String("target", defaultTarget, "IP address to ping through every interface")
	interval := fs.Duration("interval", defaultInterval, "how often every interface is re-measured")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	ip := net.ParseIP(*target)
	if ip == nil {
		return config{}, fmt.Errorf("target %q is not an IP address", *target)
	}
	if *interval <= 0 {
		return config{}, fmt.Errorf("interval must be positive, got %s", *interval)
	}
	return config{target: ip, interval: *interval}, nil
}

// source re-lists interfaces on every refresh, so an adapter plugged in mid-run
// joins the ranking without a restart. cfg carries the target and, in tests, a
// fake pinger.
func source(cfg probe.Config) ui.Source {
	return func(context.Context) ([]probe.ProbeResult, error) {
		ifaces, err := iface.ListInterfaces()
		if err != nil {
			return nil, err
		}
		return score.Rank(probe.ProbeAll(ifaces, cfg)), nil
	}
}

func permissionHint() string {
	if runtime.GOOS == "windows" {
		return "hint: raw ICMP needs an Administrator terminal"
	}
	return "hint: run with sudo, or grant the binary the capability once: sudo setcap cap_net_raw+ep bin/andromeda"
}
