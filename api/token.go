package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

func getToken(c *gin.Context) {
	refresh := c.Query("refresh_ui")
	if refresh == "" {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          "refresh_ui parameter is required",
		})
		return
	}
	handleToken(c, map[string]interface{}{"refresh_token": refresh})
}

func postToken(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.RefreshToken == "" {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          "refresh_token parameter is required",
		})
		return
	}
	handleToken(c, map[string]interface{}{"refresh_token": body.RefreshToken})
}

func handleToken(c *gin.Context, body map[string]interface{}) {
	requestInfo, err := GenerateRequestInfo("/v4/token", body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp, err := client.R().
		SetHeaders(requestInfo["headers"].(map[string]string)).
		SetBody(requestInfo["body"].(map[string]interface{})).
		Post("https://api.extscreen.com/aliyundrive/v4/token")
	if err != nil || resp.StatusCode() != 200 {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          "Failed to refresh token",
		})
		return
	}
	var tokenData map[string]interface{}
	json.Unmarshal(resp.Body(), &tokenData)

	data := tokenData["data"].(map[string]interface{})
	plain, err := Decrypt(data["ciphertext"].(string), data["iv"].(string), requestInfo["key"].(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          err.Error(),
		})
		return
	}
	var token map[string]string
	json.Unmarshal([]byte(plain), &token)

	c.JSON(http.StatusOK, gin.H{
		"refresh_token": token["refresh_token"],
		"access_token":  token["access_token"],
		"text":          "",
	})
}
