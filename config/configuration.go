package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	NetworkModeIPv4 = "ipv4"
	NetworkModeDual = "dual"
)

type Configuration struct {
	NodeRole     string
	WorkerURLs   string `form:"WorkerURLs"`
	ClusterToken string `form:"ClusterToken"`
	WorkerID      string `form:"WorkerID"`
	WorkerQueue   int
	WorkerBatch   int
	WorkerCacheDir string
	MasterURL     string
	DbName       string
	DatabaseType string
	DatabaseUrl  string
	Address      string
	MaxNeighbors uint          `form:"MaxNeighbors"`
	MaxLeeches   int           `form:"MaxLeeches"`
	DrainTimeout time.Duration `form:"DrainTimeout"`

	TelegramToken    string `form:"TelegramToken"`
	TelegramUsername string `form:"TelegramUsername"`

	DiscordWebhook string `form:"DiscordWebhook"`

	SlackWebhook string `form:"SlackWebhook"`

	GotifyURL   string `form:"GotifyURL"`
	GotifyToken string `form:"GotifyToken"`

	SafeMode bool `form:"SafeMode"`

	CrawlerThreads         int    `form:"CrawlerThreads"`
	MaxConcurrentDownloads int    `form:"MaxConcurrentDownloads"`
	RateLimit              int    `form:"RateLimit"`
	CrawlerAutoStopMinutes int    `form:"CrawlerAutoStopMinutes"`
	CrawlerStartOnLaunch   bool   `form:"CrawlerStartOnLaunch"`
	CrawlerScheduleEnabled bool   `form:"CrawlerScheduleEnabled"`
	CrawlerScheduleStart   string `form:"CrawlerScheduleStart"`
	CrawlerScheduleEnd     string `form:"CrawlerScheduleEnd"`
	MaxSavedTorrents       int    `form:"MaxSavedTorrents"`
	NetworkMode            string `form:"NetworkMode"`
	ListenIPv4             string `form:"ListenIPv4"`
	ListenIPv6             string `form:"ListenIPv6"`
	RoutingTableCacheIPv4  string `form:"RoutingTableCacheIPv4"`
	RoutingTableCacheIPv6  string `form:"RoutingTableCacheIPv6"`
	BootstrapIPv4          []string
	ConfigFile             string

	EnableBlacklist    bool   `form:"EnableBlacklist"`
	NameBlacklist      string `form:"NameBlacklist"`
	FileBlacklist      string `form:"FileBlacklist"`
	OnlyChineseContent bool   `form:"OnlyChineseContent"`

	Statistics bool `form:"Statistics"`

	BootstrapNodeFile string `form:"BootstrapNodeFile"`

	OnlyWebServer bool
	AuthUser      string
	AuthPass      string
	SessionSecret string

	TransmissionURL  string `form:"TransmissionURL"`
	TransmissionUser string `form:"TransmissionUser"`
	TransmissionPass string `form:"TransmissionPass"`

	Aria2URL   string `form:"Aria2URL"`
	Aria2Token string `form:"Aria2Token"`

	DelugeURL  string `form:"DelugeURL"`
	DelugePass string `form:"DelugePass"`

	QBittorrentURL  string `form:"QBittorrentURL"`
	QBittorrentUser string `form:"QBittorrentUser"`
	QBittorrentPass string `form:"QBittorrentPass"`
}

func ParseArguments() *Configuration {
	config := Configuration{}
	configFile := argumentValue("config")
	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err == nil {
			var fileConfig struct {
				Network struct {
					Mode string `yaml:"mode"`
				} `yaml:"network"`
				Listen struct {
					IPv4 string `yaml:"ipv4"`
					IPv6 string `yaml:"ipv6"`
				} `yaml:"listen"`
				Bootstrap struct {
					IPv4 []string `yaml:"ipv4"`
				} `yaml:"bootstrap"`
				RoutingCache struct {
					IPv4 string `yaml:"ipv4"`
					IPv6 string `yaml:"ipv6"`
				} `yaml:"routing_cache"`
				Crawler struct {
					Schedule struct {
						Enabled bool   `yaml:"enabled"`
						Start   string `yaml:"start"`
						End     string `yaml:"end"`
					} `yaml:"schedule"`
				} `yaml:"crawler"`
			}
			if yaml.Unmarshal(data, &fileConfig) == nil {
				config.NetworkMode = fileConfig.Network.Mode
				config.ListenIPv4 = fileConfig.Listen.IPv4
				config.ListenIPv6 = fileConfig.Listen.IPv6
				config.BootstrapIPv4 = fileConfig.Bootstrap.IPv4
				config.RoutingTableCacheIPv4 = fileConfig.RoutingCache.IPv4
				config.RoutingTableCacheIPv6 = fileConfig.RoutingCache.IPv6
				config.CrawlerScheduleEnabled = fileConfig.Crawler.Schedule.Enabled
				config.CrawlerScheduleStart = fileConfig.Crawler.Schedule.Start
				config.CrawlerScheduleEnd = fileConfig.Crawler.Schedule.End
			}
		}
	}
	defaultString := func(value, fallback string) string {
		if value != "" {
			return value
		}
		return fallback
	}
	defaultRole := "standalone"
	if strings.Contains(strings.ToLower(filepath.Base(os.Args[0])), "dhtc-worker") {
		defaultRole = "worker"
	}

	flag.StringVar(&config.NodeRole, "node-role", defaultRole, "node role (standalone, master, worker)")
	flag.StringVar(&config.WorkerURLs, "worker-urls", "", "comma-separated public worker URLs for master polling")
	flag.StringVar(&config.ClusterToken, "cluster-token", os.Getenv("DHTC_CLUSTER_TOKEN"), "shared token for worker authentication")
	flag.StringVar(&config.WorkerID, "worker-id", os.Getenv("DHTC_WORKER_ID"), "stable worker identifier")
	flag.IntVar(&config.WorkerQueue, "worker-queue", 256, "maximum metadata records buffered by a worker")
	flag.IntVar(&config.WorkerBatch, "worker-batch", 16, "maximum metadata records per worker upload")
	flag.StringVar(&config.WorkerCacheDir, "worker-cache-dir", "", "worker persistent cache directory (default: same as -address)")
	flag.StringVar(&config.MasterURL, "master-url", "", "master URL for worker push mode (worker pushes instead of master pull)")

	flag.StringVar(&config.DbName, "database", "dhtdb", "database name (for CloverDB)")
	flag.StringVar(&config.DatabaseType, "database-type", "clover", "database type (clover, sqlite, postgres, mysql)")
	flag.StringVar(&config.DatabaseUrl, "database-url", "", "database URL (for GORM backends)")
	flag.StringVar(&config.Address, "address", ":4200", "address to run on")
	flag.UintVar(&config.MaxNeighbors, "MaxNeighbors", 500, "max. indexer neighbors")
	flag.IntVar(&config.MaxLeeches, "MaxLeeches", 128, "max. leeches")
	flag.DurationVar(&config.DrainTimeout, "DrainTimeout", 5*time.Second, "drain timeout")

	flag.StringVar(&config.TelegramToken, "TelegramToken", "", "bot token for notifications")
	flag.StringVar(&config.TelegramUsername, "TelegramUsername", "", "username to send notifications to")

	flag.StringVar(&config.DiscordWebhook, "DiscordWebhook", "", "Discord webhook URL")

	flag.StringVar(&config.SlackWebhook, "SlackWebhook", "", "Slack webhook URL")

	flag.StringVar(&config.GotifyURL, "GotifyURL", "", "Gotify URL")
	flag.StringVar(&config.GotifyToken, "GotifyToken", "", "Gotify token")

	flag.BoolVar(&config.SafeMode, "SafeMode", false, "start with safe mode enabled")
	flag.IntVar(&config.CrawlerThreads, "CrawlerThreads", 2, "dht crawler threads")
	flag.IntVar(&config.MaxConcurrentDownloads, "MaxConcurrentDownloads", 10, "max. concurrent metadata downloads")
	flag.IntVar(&config.RateLimit, "RateLimit", 100, "max. outgoing UDP packets per second per crawler")
	flag.IntVar(&config.CrawlerAutoStopMinutes, "CrawlerAutoStopMinutes", 0, "automatically stop crawler after N minutes (0 disables auto-stop)")
	flag.BoolVar(&config.CrawlerStartOnLaunch, "CrawlerStartOnLaunch", false, "start crawler automatically when the app launches")
	flag.BoolVar(&config.CrawlerScheduleEnabled, "CrawlerScheduleEnabled", false, "run crawler only during the configured daily time window")
	flag.StringVar(&config.CrawlerScheduleStart, "CrawlerScheduleStart", defaultString(config.CrawlerScheduleStart, "22:00"), "daily crawler schedule start time (HH:MM)")
	flag.StringVar(&config.CrawlerScheduleEnd, "CrawlerScheduleEnd", defaultString(config.CrawlerScheduleEnd, "07:00"), "daily crawler schedule end time (HH:MM)")
	flag.IntVar(&config.MaxSavedTorrents, "MaxSavedTorrents", 20000, "maximum saved torrents before pruning oldest entries (0 disables pruning)")
	flag.StringVar(&config.ConfigFile, "config", configFile, "optional YAML configuration file")
	flag.StringVar(&config.NetworkMode, "NetworkMode", defaultString(config.NetworkMode, "dual"), "DHT network mode (ipv4, dual)")
	flag.StringVar(&config.ListenIPv4, "ListenIPv4", defaultString(config.ListenIPv4, "0.0.0.0:0"), "IPv4 DHT listen address")
	flag.StringVar(&config.ListenIPv6, "ListenIPv6", defaultString(config.ListenIPv6, "[::]:0"), "IPv6 DHT listen address")
	flag.StringVar(&config.RoutingTableCacheIPv4, "RoutingTableCacheIPv4", defaultString(config.RoutingTableCacheIPv4, "routing-table-v4.json"), "IPv4 routing table cache")
	flag.StringVar(&config.RoutingTableCacheIPv6, "RoutingTableCacheIPv6", defaultString(config.RoutingTableCacheIPv6, "routing-table-v6.json"), "IPv6 routing table cache")

	flag.BoolVar(&config.EnableBlacklist, "EnableBlacklist", false, "enable blacklists")
	flag.StringVar(&config.NameBlacklist, "NameBlacklist", "", "blacklist for torrent names")
	flag.StringVar(&config.FileBlacklist, "FileBlacklist", "", "blacklist for file names")
	flag.BoolVar(&config.OnlyChineseContent, "OnlyChineseContent", false, "only store torrents whose name or file paths contain Chinese characters")

	flag.BoolVar(&config.Statistics, "Statistics", false, "enable Statistics (dashboard)")

	flag.StringVar(&config.BootstrapNodeFile, "BootstrapNodeFile", "bootstrap-nodes.txt", "bootstrap nodes to use")

	flag.BoolVar(&config.OnlyWebServer, "OnlyWebServer", false, "only start the web-server")
	flag.StringVar(&config.AuthUser, "auth-user", "", "username for basic auth")
	flag.StringVar(&config.AuthPass, "auth-pass", "", "password for basic auth")
	flag.StringVar(&config.SessionSecret, "session-secret", "dhtc-secret", "session secret")

	flag.StringVar(&config.TransmissionURL, "transmission-url", "", "Transmission RPC URL (e.g. http://localhost:9091/transmission/rpc)")
	flag.StringVar(&config.TransmissionUser, "transmission-user", "", "Transmission RPC username")
	flag.StringVar(&config.TransmissionPass, "transmission-pass", "", "Transmission RPC password")

	flag.StringVar(&config.Aria2URL, "aria2-url", "", "Aria2 RPC URL (e.g. http://localhost:6800/jsonrpc)")
	flag.StringVar(&config.Aria2Token, "aria2-token", "", "Aria2 RPC secret token")

	flag.StringVar(&config.DelugeURL, "deluge-url", "", "Deluge JSON-RPC URL (e.g. http://localhost:8112/json)")
	flag.StringVar(&config.DelugePass, "deluge-pass", "", "Deluge password")

	flag.StringVar(&config.QBittorrentURL, "qbittorrent-url", "", "qBittorrent URL (e.g. http://localhost:8080)")
	flag.StringVar(&config.QBittorrentUser, "qbittorrent-user", "", "qBittorrent username")
	flag.StringVar(&config.QBittorrentPass, "qbittorrent-pass", "", "qBittorrent password")

	flag.Parse()
	config.NetworkMode = NormalizeNetworkMode(config.NetworkMode)
	config.NodeRole = NormalizeNodeRole(config.NodeRole)

	return &config
}

func NormalizeNodeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "master":
		return "master"
	case "worker":
		return "worker"
	default:
		return "standalone"
	}
}

func argumentValue(name string) string {
	prefix := "-" + name + "="
	for i, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if arg == "-"+name && i+2 <= len(os.Args[1:]) {
			return os.Args[i+2]
		}
	}
	return ""
}

func NormalizeNetworkMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case NetworkModeDual:
		return NetworkModeDual
	case "ipv6":
		return NetworkModeDual
	default:
		return NetworkModeIPv4
	}
}
