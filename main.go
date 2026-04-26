package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	// ✅ 生产环境使用 release 模式
	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	// ✅ 设置信任的代理（消除安全警告）
	r.SetTrustedProxies(nil) // 或设置为 Vercel 的代理地址

	// 你的路由
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello"})
	})
	r.GET("/qr", getQRCode)
	r.GET("/check", checkStatus)
	r.GET("/token", getToken)
	r.POST("/token", postToken)

	// ✅ 关键修复：读取 PORT 环境变量
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081" // 本地开发时的默认端口
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
