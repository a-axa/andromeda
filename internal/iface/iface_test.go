package iface

import (
	"net"
	"testing"
)

func cidr(s string) *net.IPNet {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	// ParseCIDR returns the network address; Interface.Addrs() carries the host IP.
	n.IP = ip
	return n
}

func raw(name string, index int, flags net.Flags, addrs ...*net.IPNet) rawInterface {
	return rawInterface{
		Interface: net.Interface{Name: name, Index: index, Flags: flags},
		Addrs:     toAddrs(addrs),
	}
}

func toAddrs(in []*net.IPNet) []net.Addr {
	out := make([]net.Addr, len(in))
	for i, a := range in {
		out[i] = a
	}
	return out
}

func TestSelectInterfaces(t *testing.T) {
	const up, down = net.FlagUp, net.Flags(0)

	tests := []struct {
		name string
		raw  []rawInterface
		want []Interface
	}{
		{
			name: "skips loopback and down",
			raw: []rawInterface{
				raw("lo", 1, up|net.FlagLoopback, cidr("127.0.0.1/8")),
				raw("eth0", 2, up, cidr("192.168.1.10/24")),
				raw("eth1", 3, down, cidr("10.0.0.5/8")),
			},
			want: []Interface{{Name: "eth0", Index: 2, IP: net.ParseIP("192.168.1.10")}},
		},
		{
			name: "skips link-local only interface",
			raw: []rawInterface{
				raw("docker0", 4, up, cidr("169.254.20.1/16")),
			},
			want: []Interface{},
		},
		{
			name: "prefers ipv4 over ipv6",
			raw: []rawInterface{
				raw("wlan0", 5, up, cidr("2001:db8::5/64"), cidr("192.168.0.7/24")),
			},
			want: []Interface{{Name: "wlan0", Index: 5, IP: net.ParseIP("192.168.0.7")}},
		},
		{
			name: "falls back to ipv6",
			raw: []rawInterface{
				raw("eth0", 2, up, cidr("fe80::1/64"), cidr("2001:db8::1/64")),
			},
			want: []Interface{{Name: "eth0", Index: 2, IP: net.ParseIP("2001:db8::1")}},
		},
		{
			name: "no interfaces",
			raw:  nil,
			want: []Interface{},
		},
		{
			name: "all down",
			raw: []rawInterface{
				raw("eth0", 2, down, cidr("192.168.1.10/24")),
				raw("eth1", 3, down, cidr("10.0.0.5/8")),
			},
			want: []Interface{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectInterfaces(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d interfaces %v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i].Name != tc.want[i].Name || got[i].Index != tc.want[i].Index || !got[i].IP.Equal(tc.want[i].IP) {
					t.Errorf("interface %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPickIPIgnoresNonIPNetAddresses(t *testing.T) {
	got := pickIP([]net.Addr{&net.IPAddr{IP: net.ParseIP("192.168.1.10")}})
	if got != nil {
		t.Fatalf("pickIP = %v, want nil", got)
	}
}
