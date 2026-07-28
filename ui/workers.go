package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (c *Controller) Workers(ctx *gin.Context) {
	h := c.getCommonH(ctx)
	h["isMaster"] = c.Configuration.NodeRole == "master"
	if c.WorkerMonitor != nil {
		h["workers"] = c.WorkerMonitor.Status()
	}
	ctx.HTML(http.StatusOK, "workers", h)
}

func (c *Controller) APIWorkers(ctx *gin.Context) {
	if c.Configuration.NodeRole != "master" || c.WorkerMonitor == nil {
		ctx.JSON(http.StatusOK, gin.H{"workers": []any{}})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"workers": c.WorkerMonitor.Status()})
}
