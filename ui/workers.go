package ui

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
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

func (c *Controller) APIWorkerPause(ctx *gin.Context) {
	if c.Configuration.NodeRole != "master" || c.WorkerMonitor == nil {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "not master"})
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.URL == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "url required"})
		return
	}
	ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.WorkerMonitor.PauseWorker(ctx2, req.URL); err != nil {
		log.Error().Err(err).Str("worker", req.URL).Msg("Failed to pause worker")
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (c *Controller) APIWorkerResume(ctx *gin.Context) {
	if c.Configuration.NodeRole != "master" || c.WorkerMonitor == nil {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "not master"})
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.URL == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "url required"})
		return
	}
	ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.WorkerMonitor.ResumeWorker(ctx2, req.URL); err != nil {
		log.Error().Err(err).Str("worker", req.URL).Msg("Failed to resume worker")
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
}
