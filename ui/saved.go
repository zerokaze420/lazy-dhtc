package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (c *Controller) ClearDataPost(ctx *gin.Context) {
	h := c.getCommonH(ctx)
	if ctx.PostForm("confirm") != "CLEAR" {
		h["clearError"] = translate(h["lang"].(string), "clear_data_confirm_failed")
		ctx.HTML(http.StatusBadRequest, "settings", h)
		return
	}

	if err := c.Database.ClearCapturedData(); err != nil {
		h["clearError"] = err.Error()
		ctx.HTML(http.StatusInternalServerError, "settings", h)
		return
	}

	ctx.Redirect(http.StatusSeeOther, "/discover")
}
