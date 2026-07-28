package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (c *Controller) Dashboard(ctx *gin.Context) {
	catDist, _ := c.Database.GetCategoryDistribution()
	h := c.getCommonH(ctx)
	h["info_hash_count"] = c.Database.GetInfoHashCount()
	ipv4, ipv6, unknown := c.Database.GetFamilyCounts()
	h["ipv4_count"] = ipv4
	h["ipv6_count"] = ipv6
	h["unknown_count"] = unknown
	h["statistics"] = c.Configuration.Statistics
	h["catDist"] = catDist
	if c.Crawler != nil {
		h["crawler"] = c.Crawler.Status()
	}
	ctx.HTML(http.StatusOK, "dashboard", h)
}
