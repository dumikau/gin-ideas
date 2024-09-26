package main

import "github.com/gin-gonic/gin"

func main() {
	router := gin.Default()
	router.Any("/*proxyPath", func(ctx *gin.Context) {
		ctx.JSON(200, gin.H{
			"header": ctx.Request.Header,
			"scheme": ctx.Request.URL.Scheme,
			"host":   ctx.Request.URL.Host,
			"path":   ctx.Request.URL.Path,
		})
	})

	router.Run("127.0.0.1:1234")
}
