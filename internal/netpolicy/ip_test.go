package netpolicy

import (
	"net"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "public IPv4", ip: "8.8.8.8", want: true},
		{name: "public IPv6", ip: "2606:4700:4700::1111", want: true},
		{name: "private IPv4", ip: "10.0.0.1"},
		{name: "shared address space", ip: "100.64.0.1"},
		{name: "documentation IPv4", ip: "203.0.113.1"},
		{name: "reserved IPv4", ip: "240.0.0.1"},
		{name: "IPv4-mapped private IPv6", ip: "::ffff:10.0.0.1"},
		{name: "NAT64 private IPv4", ip: "64:ff9b::a00:1"},
		{name: "6to4 transition", ip: "2002:0a00:0001::1"},
		{name: "documentation IPv6", ip: "2001:db8::1"},
		{name: "expanded documentation IPv6", ip: "3fff::1"},
		{name: "IPv6 dummy prefix", ip: "100:0:0:1::1"},
		{name: "SRv6 SID prefix", ip: "5f00::1"},
		{name: "malformed", ip: "not-an-ip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPublicIP(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("IsPublicIP(%q) = %t, want %t", tt.ip, got, tt.want)
			}
		})
	}
}
