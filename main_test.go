package main

import (
	"context"
	"math"
	"net"
	"testing"
	"time"

	"andromeda/internal/iface"
	"andromeda/internal/probe"
	"andromeda/internal/score"
)

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags(nil): %v", err)
	}
	if got, want := cfg.target.String(), "8.8.8.8"; got != want {
		t.Errorf("target = %s, want %s", got, want)
	}
	if cfg.interval != 2*time.Second {
		t.Errorf("interval = %s, want 2s", cfg.interval)
	}
}

func TestParseFlagsCustom(t *testing.T) {
	cfg, err := parseFlags([]string{"-target", "1.1.1.1", "-interval", "5s"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.target.Equal(net.ParseIP("1.1.1.1")) {
		t.Errorf("target = %s, want 1.1.1.1", cfg.target)
	}
	if cfg.interval != 5*time.Second {
		t.Errorf("interval = %s, want 5s", cfg.interval)
	}
}

func TestParseFlagsAcceptsIPv6(t *testing.T) {
	cfg, err := parseFlags([]string{"-target", "2606:4700:4700::1111"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.target.To4() != nil {
		t.Errorf("parsed %s as IPv4", cfg.target)
	}
}

func TestParseFlagsRejectsHostname(t *testing.T) {
	if _, err := parseFlags([]string{"-target", "one.one.one.one"}); err == nil {
		t.Fatal("hostname accepted; only literal IPs are probeable")
	}
}

func TestParseFlagsIntervalValidation(t *testing.T) {
	for _, tc := range []struct {
		in      string
		wantErr bool
	}{
		{in: "500ms", wantErr: false},
		{in: "0", wantErr: true},
		{in: "-1s", wantErr: true},
	} {
		if _, err := parseFlags([]string{"-interval", tc.in}); (err != nil) != tc.wantErr {
			t.Errorf("interval %s: err = %v, wantErr %v", tc.in, err, tc.wantErr)
		}
	}
}

func TestParseFlagsRejectsUnknownFlag(t *testing.T) {
	if _, err := parseFlags([]string{"-nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestPermissionHintMentionsPlatform(t *testing.T) {
	if got := permissionHint(); got == "" {
		t.Fatal("empty permission hint")
	}
}

// TestSourceWiresThePipeline exercises iface, probe, and score together with a
// fake pinger, so the wiring is checked without raw sockets or a real network.
func TestSourceWiresThePipeline(t *testing.T) {
	ifaces, err := iface.ListInterfaces()
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	if len(ifaces) == 0 {
		t.Skip("no probeable interface on this machine")
	}

	cfg := probe.Config{
		Target: net.ParseIP("8.8.8.8"),
		Count:  2,
		// Odd last address byte answers fast, even answers slow, so the ranking
		// has two distinct groups to sort.
		Ping: func(_ context.Context, src, _ net.IP) (time.Duration, error) {
			if src[len(src)-1]%2 == 0 {
				return 200 * time.Millisecond, nil
			}
			return 5 * time.Millisecond, nil
		},
	}

	results, err := source(cfg)(context.Background())
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(results) != len(ifaces) {
		t.Fatalf("got %d results, want %d", len(results), len(ifaces))
	}

	prev := math.Inf(1)
	for i, r := range results {
		if r.Interface.Name == "" || r.Interface.IP == nil {
			t.Errorf("result %d has no interface identity: %+v", i, r.Interface)
		}
		if r.PacketLossPct != 0 {
			t.Errorf("%s: fake pinger never failed but loss = %v", r.Interface.Name, r.PacketLossPct)
		}
		s := score.Score(r)
		if s > prev {
			t.Errorf("%s scores %.1f after %.1f, ranking is not descending", r.Interface.Name, s, prev)
		}
		prev = s
	}
}
