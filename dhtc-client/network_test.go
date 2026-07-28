package dhtc_client

import (
	"context"
	"net/netip"
	"testing"
)

func TestResolveBootstrapAddressesFiltersAddressFamily(t *testing.T) {
	tests := []struct {
		network string
		node    string
		wantIP  netip.Addr
	}{
		{network: "udp4", node: "127.0.0.1:6881", wantIP: netip.MustParseAddr("127.0.0.1")},
		{network: "udp6", node: "[::1]:6881", wantIP: netip.MustParseAddr("::1")},
	}

	for _, test := range tests {
		t.Run(test.network, func(t *testing.T) {
			addrs, err := resolveBootstrapAddresses(test.network, test.node)
			if err != nil {
				t.Fatal(err)
			}
			if len(addrs) != 1 || addrs[0].Addr() != test.wantIP || addrs[0].Port() != 6881 {
				t.Fatalf("unexpected addresses: %#v", addrs)
			}
		})
	}
}

func TestTransportBindsIPv4AndIPv6(t *testing.T) {
	tests := []struct {
		network string
		address string
	}{
		{network: "udp4", address: "127.0.0.1:0"},
		{network: "udp6", address: "[::1]:0"},
	}

	for _, test := range tests {
		t.Run(test.network, func(t *testing.T) {
			transport := NewTransport(test.network, test.address, 0, func(*Message, netip.AddrPort) {}, func() {})
			transport.Start(context.Background())
			transport.Terminate()
		})
	}
}
