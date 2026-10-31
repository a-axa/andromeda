package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"andromeda/internal/iface"
)

func testIface(name, ip string) iface.Interface {
	return iface.Interface{Name: name, IP: net.ParseIP(ip)}
}

func TestProbeAllAggregation(t *testing.T) {
	good := testIface("eth0", "10.0.0.2")
	dead := testIface("wlan0", "10.0.0.3")

	cfg := Config{
		Target:  net.ParseIP("8.8.8.8"),
		Count:   4,
		Timeout: time.Second,
		Ping: func(_ context.Context, src, _ net.IP) (time.Duration, error) {
			if src.Equal(good.IP) {
				return 20 * time.Millisecond, nil
			}
			return 0, errors.New("i/o timeout")
		},
	}

	got := ProbeAll([]iface.Interface{good, dead}, cfg)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}

	if got[0].Interface.Name != "eth0" || got[0].LatencyMs != 20 || got[0].PacketLossPct != 0 {
		t.Errorf("eth0 = %+v, want 20ms 0%% loss", got[0])
	}
	if got[1].Interface.Name != "wlan0" || got[1].LatencyMs != 0 || got[1].PacketLossPct != 100 {
		t.Errorf("wlan0 = %+v, want 0ms 100%% loss", got[1])
	}
}

func TestProbeAllPartialLoss(t *testing.T) {
	var calls atomic.Int32
	cfg := Config{
		Target:  net.ParseIP("1.1.1.1"),
		Count:   4,
		Timeout: time.Second,
		Ping: func(_ context.Context, _, _ net.IP) (time.Duration, error) {
			if calls.Add(1) == 4 {
				return 0, errors.New("lost")
			}
			return 10 * time.Millisecond, nil
		},
	}

	got := ProbeAll([]iface.Interface{testIface("eth0", "10.0.0.2")}, cfg)
	if got[0].PacketLossPct != 25 {
		t.Errorf("packet loss = %v%%, want 25%%", got[0].PacketLossPct)
	}
	if got[0].LatencyMs != 10 {
		t.Errorf("latency = %vms, want 10ms (average of the replies only)", got[0].LatencyMs)
	}
}

func TestProbeAllRunsInParallel(t *testing.T) {
	var ifaces []iface.Interface
	for i := range 4 {
		ifaces = append(ifaces, testIface(fmt.Sprintf("eth%d", i), fmt.Sprintf("10.0.0.%d", i+2)))
	}

	cfg := Config{
		Count:   1,
		Timeout: time.Second,
		Ping: func(context.Context, net.IP, net.IP) (time.Duration, error) {
			time.Sleep(50 * time.Millisecond)
			return time.Millisecond, nil
		},
	}

	start := time.Now()
	ProbeAll(ifaces, cfg)
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Errorf("4 interfaces took %v, want parallel (~50ms not ~200ms)", elapsed)
	}
}

func TestProbeAllPassesTimeoutToPing(t *testing.T) {
	cfg := Config{
		Count:   1,
		Timeout: 250 * time.Millisecond,
		Ping: func(ctx context.Context, _, _ net.IP) (time.Duration, error) {
			dl, ok := ctx.Deadline()
			if !ok {
				t.Error("ping context has no deadline")
			} else if remaining := time.Until(dl); remaining > 250*time.Millisecond {
				t.Errorf("deadline %v away, want <= 250ms", remaining)
			}
			return time.Millisecond, nil
		},
	}

	ProbeAll([]iface.Interface{testIface("eth0", "10.0.0.2")}, cfg)
}

func TestConfigDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got.Count != 3 || got.Timeout != 2*time.Second || got.Ping == nil {
		t.Errorf("defaults = %+v, want 3 samples, 2s timeout, ICMPPing", got)
	}

	custom := Config{Count: 7, Timeout: time.Second}
	if c := custom.withDefaults(); c.Count != 7 || c.Timeout != time.Second {
		t.Errorf("explicit config was overwritten: %+v", c)
	}
}
