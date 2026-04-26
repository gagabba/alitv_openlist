package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var app *gin.Engine

func init() {
	// 切换到 release 模式，消除 debug 警告
	gin.SetMode(gin.ReleaseMode)

	app = gin.New()
	app.Use(gin.Recovery())

	// 设置受信任的代理（消除 trusted proxies 警告）
	app.SetTrustedProxies(nil)

	// ===== 把你 main.go 里的路由全部搬到这里 =====
	app.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello"})
	})
	app.GET("/qr", getQRCode)
	app.GET("/check", checkStatus)
	app.GET("/token", getToken)
	app.POST("/token", postToken)
	// ===== 路由结束 =====

	// ⚠️ 注意：这里绝对不能调用 app.Run()
}

// Handler 是 Vercel 调用的入口函数
func Handler(w http.ResponseWriter, r *http.Request) {
	app.ServeHTTP(w, r)
}
