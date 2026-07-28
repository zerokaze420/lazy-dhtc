package ui

import (
	"dhtc/config"
	"testing"
)

func TestCrawlerEndpoints(t *testing.T) {
	tests := []struct {
		mode     string
		networks []string
	}{
		{mode: "ipv4", networks: []string{"udp4"}},
		{mode: "ipv6", networks: []string{"udp6"}},
		{mode: "dual", networks: []string{"udp4", "udp6"}},
		{mode: "invalid", networks: []string{"udp4"}},
	}

	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			endpoints := crawlerEndpoints(&config.Configuration{NetworkMode: test.mode, ListenIPv4: "0.0.0.0:0", ListenIPv6: "[::]:0"}, nil, nil)
			if len(endpoints) != len(test.networks) {
				t.Fatalf("got %d endpoints, want %d", len(endpoints), len(test.networks))
			}
			for i, network := range test.networks {
				if endpoints[i].Network != network {
					t.Fatalf("endpoint %d network = %q, want %q", i, endpoints[i].Network, network)
				}
			}
		})
	}
}

func TestCrawlerEndpointsCreatesOneNetworkPerAddressFamily(t *testing.T) {
	endpoints := crawlerEndpoints(&config.Configuration{NetworkMode: "dual", ListenIPv4: "0.0.0.0:6881", ListenIPv6: "[::]:6881"}, nil, nil)
	if len(endpoints) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(endpoints))
	}
	want := []string{"udp4", "udp6"}
	for i, network := range want {
		if endpoints[i].Network != network {
			t.Fatalf("endpoint %d network = %q, want %q", i, endpoints[i].Network, network)
		}
	}
}

func TestCrawlerEndpointsIncludeConfiguredIPv6BootstrapNodes(t *testing.T) {
	configuration := &config.Configuration{
		NetworkMode:        "ipv6",
		ListenIPv6:         "[::]:6881",
		BootstrapNodesIPv6: "[2001:db8::1]:6881\nseed.example:6881, [2001:db8::1]:6881",
	}
	endpoints := crawlerEndpoints(configuration, nil, nil)
	if len(endpoints) != 1 || len(endpoints[0].Bootstrap) != 2 {
		t.Fatalf("IPv6 bootstrap nodes = %#v, want two unique nodes", endpoints)
	}
}
