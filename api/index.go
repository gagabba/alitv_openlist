// api/index.go
package handler

import (
    "net/http"
    "github.com/gin-gonic/gin"
)

var app *gin.Engine

func init() {
    gin.SetMode(gin.ReleaseMode)
    app = gin.Default()
    
    // 把你原来 main.go 里的路由注册都搬到这里
    app.GET("/", func(c *gin.Context) {
        // 你原来的逻辑
    })
    app.GET("/qr", getQRCode)
    app.GET("/check", checkStatus)
    app.GET("/token", getToken)
    app.POST("/token", postToken)
}

// 这个函数是 Vercel 调用的入口，注意函数名大写开头
func Handler(w http.ResponseWriter, r *http.Request) {
    app.ServeHTTP(w, r)
}
