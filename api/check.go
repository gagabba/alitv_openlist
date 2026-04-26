package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

func checkStatus(c *gin.Context) {
	sid := c.Query("sid")
	if sid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sid"})
		return
	}
	statusResp, err := client.R().
		Get("https://openapi.alipan.com/oauth/qrcode/" + sid + "/status")
	if err != nil || statusResp.StatusCode() != 200 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check status"})
		return
	}
	var statusData map[string]interface{}
	json.Unmarshal(statusResp.Body(), &statusData)

	if statusData["status"] == "LoginSuccess" {
		authCode := statusData["authCode"].(string)
		handleToken(c, map[string]interface{}{"code": authCode})
		return
	}
	c.JSON(http.StatusOK, statusData)
}
