package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (c *Controller) BlacklistGet(ctx *gin.Context) {
	h := c.getCommonH(ctx)
	h["results"] = c.Database.GetBlacklistEntries()
	ctx.HTML(http.StatusOK, "blacklist", h)
}

func (c *Controller) BlacklistPost(ctx *gin.Context) {
	opOk := false
	op := ctx.PostForm("op")
	if op == "add" {
		opOk = c.Database.AddToBlacklist([]string{ctx.PostForm("Filter")}, ctx.PostForm("Type"))
	} else if op == "delete" {
		opOk = c.Database.DeleteBlacklistItem(ctx.PostForm("Id")) == nil
	} else if op == "enable" {
		c.Configuration.EnableBlacklist = true
		opOk = true
	} else if op == "disable" {
		c.Configuration.EnableBlacklist = false
		opOk = true
	}

	h := c.getCommonH(ctx)
	h["op"] = op
	h["opOk"] = opOk
	h["results"] = c.Database.GetBlacklistEntries()
	ctx.HTML(http.StatusOK, "blacklist", h)
}
