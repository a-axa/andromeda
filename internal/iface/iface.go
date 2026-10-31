// Package iface lists the network interfaces available for probing.
package iface

import (
	"fmt"
	"net"
)

// Interface is one network interface Andromeda can probe through.
type Interface struct {
	Name  string
	Index int
	IP    net.IP
}

// ListInterfaces returns interfaces that are up, not loopback, and hold a
// routable address. Interfaces with no address are skipped: there is nothing
// to bind a probe to.
func ListInterfaces() ([]Interface, error) {
	nifs, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	raw := make([]rawInterface, 0, len(nifs))
	for _, nif := range nifs {
		addrs, err := nif.Addrs()
		if err != nil {
			return nil, fmt.Errorf("addresses for %s: %w", nif.Name, err)
		}
		raw = append(raw, rawInterface{Interface: nif, Addrs: addrs})
	}

	return selectInterfaces(raw), nil
}

// rawInterface carries the address lookup next to the interface so selection
// stays a pure function that tests can feed by hand.
type rawInterface struct {
	net.Interface
	Addrs []net.Addr
}

func selectInterfaces(raw []rawInterface) []Interface {
	out := make([]Interface, 0, len(raw))
	for _, r := range raw {
		if r.Flags&net.FlagUp == 0 || r.Flags&net.FlagLoopback != 0 {
			continue
		}
		ip := pickIP(r.Addrs)
		if ip == nil {
			continue
		}
		out = append(out, Interface{Name: r.Name, Index: r.Index, IP: ip})
	}
	return out
}

// pickIP prefers IPv4 and falls back to IPv6, skipping loopback and link-local
// addresses, which cannot reach a public target.
func pickIP(addrs []net.Addr) net.IP {
	var v6 net.IP
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.To4() != nil {
			return ip
		}
		if v6 == nil {
			v6 = ip
		}
	}
	return v6
}
