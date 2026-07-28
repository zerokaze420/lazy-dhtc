package ui

import (
	"dhtc/config"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (c *Controller) SettingsGet(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "settings", c.getCommonH(ctx))
}

func (c *Controller) SettingsPost(ctx *gin.Context) {
	if err := ctx.ShouldBind(c.Configuration); err != nil {
		h := c.getCommonH(ctx)
		h["error"] = err.Error()
		ctx.HTML(http.StatusBadRequest, "settings", h)
		return
	}
	c.Configuration.NetworkMode = config.NormalizeNetworkMode(c.Configuration.NetworkMode)
	if c.Configuration.CrawlerScheduleEnabled {
		start, startErr := time.Parse("15:04", c.Configuration.CrawlerScheduleStart)
		end, endErr := time.Parse("15:04", c.Configuration.CrawlerScheduleEnd)
		if startErr != nil || endErr != nil || start.Equal(end) {
			h := c.getCommonH(ctx)
			h["error"] = "定时捕获的开始和停止时间必须有效且不能相同"
			ctx.HTML(http.StatusBadRequest, "settings", h)
			return
		}
	}
	if c.Notifier != nil {
		c.Notifier.Setup(c.Configuration)
	}
	if c.Crawler != nil {
		c.Crawler.ReconcileSchedule()
	}
	if c.WorkerMonitor != nil {
		c.WorkerMonitor.Configure(splitWorkerURLs(c.Configuration.WorkerURLs), c.Configuration.ClusterToken)
	}
	h := c.getCommonH(ctx)
	h["saved"] = true
	ctx.HTML(http.StatusOK, "settings", h)
}

func splitWorkerURLs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
}
