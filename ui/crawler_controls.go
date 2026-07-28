package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (c *Controller) CrawlerStart(ctx *gin.Context) {
	if c.Crawler != nil {
		c.Crawler.Start()
	}
	ctx.Redirect(http.StatusSeeOther, "/dashboard")
}

func (c *Controller) CrawlerStop(ctx *gin.Context) {
	if c.Crawler != nil {
		c.Crawler.Stop()
	}
	ctx.Redirect(http.StatusSeeOther, "/dashboard")
}
