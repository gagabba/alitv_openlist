package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

var client = resty.New()

//go:embed index.html
var indexHtml []byte

func randomString(length int) string {
	if length <= 0 {
		length = 32
	}
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	rand.Seed(time.Now().UnixNano())
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}

func getSign(apiPath string, t string) string {
	params := GetParams(t)
	key := GenerateKey(t)
	data := fmt.Sprintf("POST-/api%v-%v-%v-%v", apiPath, t, params["d"], key)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func Encrypt(plaintextStr, ivHex, keyStr string) (string, error) {
	key := []byte(keyStr)
	if len(key) != 32 {
		return "", errors.New("key 长度必须为 32 字节（AES-256）")
	}
	iv := []byte(ivHex)
	if len(iv) != aes.BlockSize {
		return "", errors.New("IV 长度必须为 16 字节（128 位）")
	}
	plaintext := pkcs7Pad([]byte(plaintextStr), aes.BlockSize)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func Decrypt(ciphertextB64, ivHex, keyStr string) (string, error) {
	key := []byte(keyStr)
	if len(key) != 32 {
		return "", errors.New("key 长度超过 32 字节，不能用于 AES-256")
	}
	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", fmt.Errorf("iv 解码失败: %w", err)
	}
	if len(iv) != aes.BlockSize {
		return "", errors.New("IV 长度必须为 16 字节（128 位）")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("密文 base64 解码失败: %w", err)
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("密文长度不是块大小的倍数")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	plaintext, err = pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return "", fmt.Errorf("去 padding 失败: %w", err)
	}
	return string(plaintext), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("无效的数据长度")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize {
		return nil, errors.New("无效的 padding 长度")
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, errors.New("padding 内容不合法")
		}
	}
	return data[:len(data)-padLen], nil
}

func h(charArray []rune, modifier interface{}) string {
	uniqueMap := make(map[rune]bool)
	var uniqueChars []rune
	for _, c := range charArray {
		if !uniqueMap[c] {
			uniqueMap[c] = true
			uniqueChars = append(uniqueChars, c)
		}
	}
	modStr := fmt.Sprintf("%v", modifier)
	if len(modStr) < 7 {
		panic("modifier 字符串长度不足7")
	}
	numericModifier, err := strconv.Atoi(modStr[7:])
	if err != nil {
		panic(err)
	}
	var builder strings.Builder
	for _, char := range uniqueChars {
		newCharCode := abs(int(char) - (numericModifier%127) - 1)
		if newCharCode < 33 {
			newCharCode += 33
		}
		builder.WriteRune(rune(newCharCode))
	}
	return builder.String()
}

func GetParams(t interface{}) map[string]string {
	return map[string]string{
		"akv":     "2.8.1496",
		"apv":     "1.4.1",
		"b":       "vivo",
		"d":       "2c7d30cd7ae5e8017384988393f397c6",
		"m":       "V2329A",
		"n":       "V2329A",
		"mac":     "",
		"wifiMac": "00db00200063",
		"nonce":   "",
		"t":       fmt.Sprintf("%v", t),
	}
}

func GenerateKey(t interface{}) string {
	params := GetParams(t)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var concatenatedParams strings.Builder
	for _, k := range keys {
		if k != "t" {
			concatenatedParams.WriteString(params[k])
		}
	}
	keyArray := []rune(concatenatedParams.String())
	hashedKeyString := h(keyArray, t)
	md5Sum := md5.Sum([]byte(hashedKeyString))
	return hex.EncodeToString(md5Sum[:])
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func getTimestamp() string {
	statusResp, err := client.R().Get("https://api.extscreen.com/timestamp")
	if err != nil || statusResp.StatusCode() != 200 {
		return strconv.FormatInt(time.Now().Unix(), 10)
	}
	var statusData map[string]interface{}
	json.Unmarshal(statusResp.Body(), &statusData)
	if statusData["code"].(float64) != 200 {
		return strconv.FormatInt(time.Now().Unix(), 10)
	}
	data := statusData["data"].(map[string]interface{})
	return strconv.FormatInt(int64(data["timestamp"].(float64)), 10)
}

func GenerateRequestInfo(apiPath string, body map[string]interface{}) (map[string]interface{}, error) {
	t := getTimestamp()
	keyStr := GenerateKey(t)
	headers := GetParams(t)
	bodyJsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	iv := randomString(16)
	encrypted, err := Encrypt(string(bodyJsonBytes), iv, keyStr)
	if err != nil {
		return nil, err
	}
	headers["Content-Type"] = "application/json"
	headers["sign"] = getSign(apiPath, t)
	return map[string]interface{}{
		"headers": headers,
		"body":    map[string]interface{}{"ciphertext": encrypted, "iv": iv},
		"key":     keyStr,
	}, nil
}

func getQRCode(c *gin.Context) {
	body := map[string]interface{}{
		"scopes": "user:base,file:all:read,file:all:write",
		"width":  500,
		"height": 500,
	}
	requestInfo, err := GenerateRequestInfo("/v2/qrcode", body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
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
	plain, err := Decrypt(data["ciphertext"].(string), data["iv"].(string), requestInfo["key"].(string))
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
		// 扫码登录流程不需要 debug，固定传 false
		handleToken(c, map[string]interface{}{"code": statusData["authCode"].(string)}, false)
		return
	}
	c.JSON(http.StatusOK, statusData)
}

// GET /token?refresh_ui=xxx[&debug=1]
func getToken(c *gin.Context) {
	refresh := c.Query("refresh_ui")
	debug := c.Query("debug") == "1"
	if refresh == "" {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          "refresh_ui parameter is required",
		})
		return
	}
	handleToken(c, map[string]interface{}{"refresh_token": refresh}, debug)
}

// POST /token  body: refresh_token=xxx[&debug=1]  或 JSON {"refresh_token":"xxx"}
func postToken(c *gin.Context) {
	// 同时支持 JSON body 和 form 表单
	refreshToken := c.PostForm("refresh_token")
	if refreshToken == "" {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		c.ShouldBindJSON(&body)
		refreshToken = body.RefreshToken
	}
	debug := c.Query("debug") == "1" || c.PostForm("debug") == "1"
	if refreshToken == "" {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          "refresh_token parameter is required",
		})
		return
	}
	handleToken(c, map[string]interface{}{"refresh_token": refreshToken}, debug)
}

// handleToken 获取 token；debug=true 时把阿里原始返回明文（token 值打码）放进 debug_plain 字段
func handleToken(c *gin.Context, body map[string]interface{}, debug bool) {
	requestInfo, err := GenerateRequestInfo("/v4/token", body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
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

	// 临时 DEBUG 日志，确认阿里返回的完整字段
	log.Printf("[DEBUG] /v4/token plain fields: %s", plain)

	var token map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &token); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"refresh_token": "",
			"access_token":  "",
			"text":          err.Error(),
		})
		return
	}

	// debug 模式：把阿里原始返回明文（凭据值打码）放进 debug_plain 字段
	if debug {
		redacted := plain
		if at, ok := token["access_token"].(string); ok && at != "" {
			redacted = strings.ReplaceAll(redacted, at, "***ACCESS_TOKEN***")
		}
		if rt, ok := token["refresh_token"].(string); ok && rt != "" {
			redacted = strings.ReplaceAll(redacted, rt, "***REFRESH_TOKEN***")
		}
		token["debug_plain"] = redacted
	}

	// 原样透传所有字段
	token["text"] = ""
	c.JSON(http.StatusOK, token)
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.SetTrustedProxies(nil)

	router.GET("/", func(c *gin.Context) {
		c.Writer.Write(indexHtml)
	})
	router.GET("/qr", getQRCode)
	router.GET("/check", checkStatus)
	router.GET("/token", getToken)
	router.POST("/token", postToken)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
