package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (c *Controller) WatchGet(ctx *gin.Context) {
	h := c.getCommonH(ctx)
	h["results"] = c.Database.GetWatchEntries()
	ctx.HTML(http.StatusOK, "watches", h)
}

func (c *Controller) WatchPost(ctx *gin.Context) {
	opOk := false
	op := ctx.PostForm("op")
	if op == "add" {
		opOk = c.Database.InsertWatchEntry(
			ctx.PostForm("key"),
			ctx.PostForm("match-type"),
			ctx.PostForm("search-input"))
	} else if op == "delete" {
		opOk = c.Database.DeleteWatchEntry(ctx.PostForm("id")) == nil
	}

	h := c.getCommonH(ctx)
	h["op"] = op
	h["opOk"] = opOk
	h["results"] = c.Database.GetWatchEntries()
	ctx.HTML(http.StatusOK, "watches", h)
}
