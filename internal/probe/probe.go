// Package probe measures latency and packet loss through each interface.
package probe

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"andromeda/internal/iface"
)

// ProbeResult is one interface's measured quality for a single run.
type ProbeResult struct {
	Interface     iface.Interface
	LatencyMs     float64
	PacketLossPct float64
}

// Pinger sends one echo from src to dst and reports the round-trip time.
type Pinger func(ctx context.Context, src, dst net.IP) (time.Duration, error)

// Config controls a probe run. Zero Count, Timeout, or Ping fall back to
// defaults.
type Config struct {
	Target  net.IP
	Count   int
	Timeout time.Duration
	Ping    Pinger
}

func (c Config) withDefaults() Config {
	if c.Count <= 0 {
		c.Count = 3
	}
	if c.Timeout <= 0 {
		c.Timeout = 2 * time.Second
	}
	if c.Ping == nil {
		c.Ping = ICMPPing
	}
	return c
}

// ProbeAll probes every interface concurrently and returns one result each, in
// the same order. A slow interface only costs its own timeout, never the run.
func ProbeAll(ifaces []iface.Interface, cfg Config) []ProbeResult {
	cfg = cfg.withDefaults()

	results := make([]ProbeResult, len(ifaces))
	var wg sync.WaitGroup
	for i, ifc := range ifaces {
		wg.Go(func() {
			results[i] = probeOne(ifc, cfg)
		})
	}
	wg.Wait()
	return results
}

// probeOne averages the samples that replied; lost samples only count toward
// packet loss, so a dead interface reports 100% loss rather than a fake 0ms.
func probeOne(ifc iface.Interface, cfg Config) ProbeResult {
	var total time.Duration
	ok := 0
	for range cfg.Count {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		d, err := cfg.Ping(ctx, ifc.IP, cfg.Target)
		cancel()
		if err != nil {
			continue
		}
		total += d
		ok++
	}

	res := ProbeResult{
		Interface:     ifc,
		PacketLossPct: float64(cfg.Count-ok) / float64(cfg.Count) * 100,
	}
	if ok > 0 {
		res.LatencyMs = float64(total.Microseconds()) / float64(ok) / 1000
	}
	return res
}

// CheckRawICMP opens one raw ICMP socket and closes it again, without any
// network I/O. Callers use it to fail early with a permission message instead
// of rendering every interface as unreachable when the process simply may not
// open raw sockets.
func CheckRawICMP() error {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("open raw ICMP socket: %w", err)
	}
	return conn.Close()
}

// ICMPPing sends a single echo request from src and waits for the reply. The
// socket binds to src, so the kernel routes the packet through that interface's
// address. Raw ICMP needs CAP_NET_RAW on Linux and administrator rights on
// Windows.
//
// shortcut: source-IP binding only, not SO_BINDTODEVICE. On a host with two
// interfaces on the same subnet the kernel may still egress elsewhere; add
// per-OS device binding if that shows up.
func ICMPPing(ctx context.Context, src, dst net.IP) (time.Duration, error) {
	var network string
	var proto int
	var reqType, replyType icmp.Type
	if src.To4() != nil {
		network, proto = "ip4:icmp", 1
		reqType, replyType = ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply
	} else {
		network, proto = "ip6:ipv6-icmp", 58
		reqType, replyType = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	}

	conn, err := icmp.ListenPacket(network, src.String())
	if err != nil {
		return 0, fmt.Errorf("listen %s on %s: %w", network, src, err)
	}
	defer conn.Close()

	id := os.Getpid() & 0xffff
	msg := icmp.Message{
		Type: reqType,
		Body: &icmp.Echo{ID: id, Seq: 1, Data: []byte("andromeda")},
	}
	payload, err := msg.Marshal(nil)
	if err != nil {
		return 0, fmt.Errorf("marshal echo: %w", err)
	}

	start := time.Now()
	if _, err := conn.WriteTo(payload, &net.IPAddr{IP: dst}); err != nil {
		return 0, fmt.Errorf("write echo to %s: %w", dst, err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = start.Add(3 * time.Second)
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return 0, fmt.Errorf("set deadline: %w", err)
	}

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, fmt.Errorf("read echo from %s: %w", dst, err)
		}
		parsed, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue
		}
		echo, isEcho := parsed.Body.(*icmp.Echo)
		if !isEcho || parsed.Type != replyType || echo.ID != id {
			continue
		}
		return time.Since(start), nil
	}
}
