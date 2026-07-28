package ui

import (
	"crypto/subtle"
	"dhtc/cluster"
	"dhtc/db"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (c *Controller) WorkerMetadataIngest(ctx *gin.Context) {
	if c.Configuration.NodeRole != "master" || c.Configuration.ClusterToken == "" {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	token := strings.TrimPrefix(ctx.GetHeader("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(c.Configuration.ClusterToken)) != 1 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var batch cluster.Batch
	if err := ctx.ShouldBindJSON(&batch); err != nil || batch.WorkerID == "" || len(batch.Items) == 0 || len(batch.Items) > 64 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid batch"})
		return
	}
	accepted := 0
	duplicates := 0
	for _, md := range batch.Items {
		if len(md.InfoHash) != 20 && len(md.InfoHash) != 32 {
			continue
		}
		if c.Database.InsertMetadata(md) {
			accepted++
			db.CheckWatches(c.Configuration, c.Database, md, c.Notifier)
			c.Hub.BroadcastMetadata(md)
		} else {
			duplicates++
		}
	}
	ctx.JSON(http.StatusOK, cluster.BatchResponse{Accepted: accepted, Duplicate: duplicates})
}
