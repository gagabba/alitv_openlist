package handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed index.html
var indexHtml []byte

var app *gin.Engine

func init() {
	gin.SetMode(gin.ReleaseMode)

	app = gin.New()
	app.Use(gin.Recovery())
	app.SetTrustedProxies(nil)

	app.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHtml)
	})

	app.GET("/qr", getQRCode)
	app.GET("/check", checkStatus)
	app.GET("/token", getToken)
	app.POST("/token", postToken)
}

// Handler 是 Vercel 的入口函数，绝对不要调用 app.Run()
func Handler(w http.ResponseWriter, r *http.Request) {
	app.ServeHTTP(w, r)
}
