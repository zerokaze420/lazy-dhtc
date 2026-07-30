package ui

import (
	"dhtc/config"
	"testing"
	"time"
)

func TestCrawlerEndpoints(t *testing.T) {
	tests := []struct {
		mode     string
		networks []string
	}{
		{mode: "ipv4", networks: []string{"udp4"}},
		{mode: "ipv6", networks: []string{"udp4", "udp6"}},
		{mode: "dual", networks: []string{"udp4", "udp6"}},
		{mode: "invalid", networks: []string{"udp4"}},
	}

	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			endpoints := crawlerEndpoints(&config.Configuration{NetworkMode: test.mode, ListenIPv4: "0.0.0.0:0", ListenIPv6: "[::]:0"}, nil)
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

func TestScheduleStateDaytimeWindow(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	inside, next := scheduleState(time.Date(2026, 7, 28, 14, 30, 0, 0, location), "09:00", "18:00")
	if !inside || next.Hour() != 18 || next.Day() != 28 {
		t.Fatalf("inside=%v next=%v, want running until 18:00", inside, next)
	}
	inside, next = scheduleState(time.Date(2026, 7, 28, 20, 0, 0, 0, location), "09:00", "18:00")
	if inside || next.Hour() != 9 || next.Day() != 29 {
		t.Fatalf("inside=%v next=%v, want stopped until next day 09:00", inside, next)
	}
}

func TestScheduleStateCrossesMidnight(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	tests := []struct {
		hour   int
		inside bool
	}{
		{hour: 23, inside: true},
		{hour: 6, inside: true},
		{hour: 12, inside: false},
	}
	for _, test := range tests {
		inside, _ := scheduleState(time.Date(2026, 7, 28, test.hour, 0, 0, 0, location), "22:00", "07:00")
		if inside != test.inside {
			t.Fatalf("hour %d inside=%v, want %v", test.hour, inside, test.inside)
		}
	}
}

func TestScheduleStateRejectsInvalidWindow(t *testing.T) {
	if inside, next := scheduleState(time.Now(), "22:00", "22:00"); inside || !next.IsZero() {
		t.Fatalf("equal schedule returned inside=%v next=%v", inside, next)
	}
}

func TestManualOverrideLastsUntilNextScheduleBoundary(t *testing.T) {
	manager := &CrawlerManager{configuration: &config.Configuration{
		CrawlerScheduleEnabled: true,
		CrawlerScheduleStart:   "22:00",
		CrawlerScheduleEnd:     "07:00",
	}}
	now := time.Date(2026, 7, 28, 18, 45, 0, 0, time.Local)
	manager.setManualOverrideAtLocked(now, true)
	if !manager.manualOverride || !manager.manualRun {
		t.Fatal("manual start did not establish a running override")
	}
	if manager.manualUntil.Hour() != 22 || manager.manualUntil.Day() != 28 {
		t.Fatalf("manual override until %v, want 22:00", manager.manualUntil)
	}
}

func TestCrawlerEndpointsCreatesOneNetworkPerAddressFamily(t *testing.T) {
	endpoints := crawlerEndpoints(&config.Configuration{NetworkMode: "dual", ListenIPv4: "0.0.0.0:6881", ListenIPv6: "[::]:6881"}, nil)
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

func TestCrawlerStatusIncludesMetrics(t *testing.T) {
	manager := &CrawlerManager{configuration: &config.Configuration{}}
	manager.discovered.Add(12)
	manager.downloadSucceeded.Add(7)
	manager.dhtOutputDropped.Add(3)
	status := manager.Status()
	if status.Discovered != 12 || status.DownloadSucceeded != 7 || status.DHTOutputDropped != 3 {
		t.Fatalf("crawler metrics = %#v", status)
	}
}
