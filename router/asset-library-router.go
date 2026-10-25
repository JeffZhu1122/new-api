package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// SetAssetLibraryRouter serves the asset library's Ark-compatible OpenAPI at
// /volc?Action=...&Version=2024-01-01 for gateway API keys.
func SetAssetLibraryRouter(router *gin.Engine) {
	volcRouter := router.Group("/volc")
	volcRouter.Use(middleware.RouteTag("api"))
	volcRouter.Use(middleware.GlobalAPIRateLimit())
	volcRouter.Use(controller.AssetLibraryTokenAuth())
	volcRouter.POST("", controller.AssetLibraryOpenAPI)
	volcRouter.POST("/", controller.AssetLibraryOpenAPI)
}
