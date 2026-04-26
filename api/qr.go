package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

func getQRCode(c *gin.Context) {
	body := map[string]interface{}{
		"scopes": "user:base,file:all:read,file:all:write",
		"width":  500,
		"height": 500,
	}
	requestInfo, err := GenerateRequestInfo("/v2/qrcode", body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp, err := client.R().
		SetHeaders(requestInfo["headers"].(map[string]string)).
		SetBody(requestInfo["body"].(map[string]interface{})).
		Post("https://api.extscreen.com/aliyundrive/v2/qrcode")
	if err != nil || resp.StatusCode() != 200 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate QR code"})
		return
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Body(), &result)
	data := result["data"].(map[string]interface{})

	respCiphertext := data["ciphertext"].(string)
	respIv := data["iv"].(string)

	plain, err := Decrypt(respCiphertext, respIv, requestInfo["key"].(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var qrcodeInfo map[string]string
	json.Unmarshal([]byte(plain), &qrcodeInfo)
	c.JSON(http.StatusOK, gin.H{
		"qr_link": qrcodeInfo["qrCodeUrl"],
		"sid":     qrcodeInfo["sid"],
	})
}
