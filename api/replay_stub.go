//go:build !dev

package api

import "github.com/gin-gonic/gin"

// augmentRoutes 在非 dev 构建中为空实现
func augmentRoutes(s *Server, api *gin.RouterGroup, protected *gin.RouterGroup) {}

