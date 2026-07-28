package ui

import "testing"

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
			endpoints := crawlerEndpoints(test.mode, 1)
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

func TestCrawlerEndpointsScaleWorkersPerAddressFamily(t *testing.T) {
	endpoints := crawlerEndpoints("dual", 2)
	if len(endpoints) != 4 {
		t.Fatalf("got %d endpoints, want 4", len(endpoints))
	}
	want := []string{"udp4", "udp4", "udp6", "udp6"}
	for i, network := range want {
		if endpoints[i].Network != network {
			t.Fatalf("endpoint %d network = %q, want %q", i, endpoints[i].Network, network)
		}
	}
}
