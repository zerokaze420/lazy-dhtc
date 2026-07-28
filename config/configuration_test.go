package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgumentsLoadsDualStackYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dhtc.yml")
	data := []byte("network:\n  mode: ipv6\nlisten:\n  ipv4: 127.0.0.1:6881\n  ipv6: '[::1]:6881'\nbootstrap:\n  ipv6:\n    - '[2001:db8::1]:6881'\nrouting_cache:\n  ipv6: /tmp/routing-v6.json\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	originalArgs := os.Args
	originalFlags := flag.CommandLine
	defer func() {
		os.Args = originalArgs
		flag.CommandLine = originalFlags
	}()
	os.Args = []string{"dhtc", "-config", path, "-NetworkMode", "dual"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)

	cfg := ParseArguments()
	if cfg.NetworkMode != NetworkModeDual {
		t.Fatalf("network mode = %q, want dual", cfg.NetworkMode)
	}
	if cfg.ListenIPv6 != "[::1]:6881" || len(cfg.BootstrapIPv6) != 1 {
		t.Fatalf("IPv6 YAML configuration not loaded: %#v", cfg)
	}
}
