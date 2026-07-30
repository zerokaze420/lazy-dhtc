package ui

import (
	"dhtc/config"
	"dhtc/downloader"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func downloaderClient(configuration *config.Configuration, name string) (downloader.TestableClient, error) {
	switch name {
	case "transmission":
		return &downloader.TransmissionClient{URL: configuration.TransmissionURL, User: configuration.TransmissionUser, Pass: configuration.TransmissionPass}, nil
	case "aria2":
		return &downloader.Aria2Client{URL: configuration.Aria2URL, Token: configuration.Aria2Token}, nil
	case "deluge":
		return &downloader.DelugeClient{URL: configuration.DelugeURL, Pass: configuration.DelugePass}, nil
	case "qbittorrent":
		return &downloader.QBittorrentClient{URL: configuration.QBittorrentURL, User: configuration.QBittorrentUser, Pass: configuration.QBittorrentPass}, nil
	default:
		return nil, fmt.Errorf("unsupported downloader %q", name)
	}
}

func configuredDownloaderURL(configuration *config.Configuration, name string) string {
	switch name {
	case "transmission":
		return configuration.TransmissionURL
	case "aria2":
		return configuration.Aria2URL
	case "deluge":
		return configuration.DelugeURL
	case "qbittorrent":
		return configuration.QBittorrentURL
	default:
		return ""
	}
}

func testConfiguredDownloaders(configuration *config.Configuration) error {
	for _, name := range []string{"transmission", "aria2", "deluge", "qbittorrent"} {
		if strings.TrimSpace(configuredDownloaderURL(configuration, name)) == "" {
			continue
		}
		client, _ := downloaderClient(configuration, name)
		if err := client.TestConnection(); err != nil {
			return fmt.Errorf("%s connection test failed: %w", name, err)
		}
	}
	return nil
}

func (c *Controller) TestDownloader(ctx *gin.Context) {
	var candidate config.Configuration
	if err := ctx.ShouldBind(&candidate); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := ctx.Param("service")
	if strings.TrimSpace(configuredDownloaderURL(&candidate, name)) == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "URL is required"})
		return
	}
	client, err := downloaderClient(&candidate, name)
	if err == nil {
		err = client.TestConnection()
	}
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": name + " connection successful"})
}

func (c *Controller) SendToTransmission(ctx *gin.Context) {
	client, _ := downloaderClient(c.Configuration, "transmission")
	c.handleDownload(ctx, client, "transmission")
}

func (c *Controller) SendToAria2(ctx *gin.Context) {
	client, _ := downloaderClient(c.Configuration, "aria2")
	c.handleDownload(ctx, client, "aria2")
}

func (c *Controller) SendToDeluge(ctx *gin.Context) {
	client, _ := downloaderClient(c.Configuration, "deluge")
	c.handleDownload(ctx, client, "deluge")
}

func (c *Controller) SendToQBittorrent(ctx *gin.Context) {
	client, _ := downloaderClient(c.Configuration, "qbittorrent")
	c.handleDownload(ctx, client, "qbittorrent")
}
