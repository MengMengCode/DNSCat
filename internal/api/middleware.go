package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/config"
	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   uint       `json:"user_id"`
	Username string     `json:"username"`
	Role     model.Role `json:"role"`
	jwt.RegisteredClaims
}

func GenerateJWT(user *model.User, secret string) (string, error) {
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   fmt.Sprintf("%d", user.ID),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		apiKeyHeader := c.GetHeader("X-API-Key")

		// 1. API Key Auth
		if apiKeyHeader != "" {
			var user model.User
			if err := database.DB.Where("api_key = ?", apiKeyHeader).First(&user).Error; err == nil {
				c.Set("user", &user)
				c.Set("user_id", user.ID)
				c.Set("role", user.Role)
				c.Next()
				return
			}
		}

		// 2. JWT Bearer Token Auth
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header missing or invalid"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(cfg.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		var user model.User
		if err := database.DB.First(&user, claims.UserID).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			return
		}

		c.Set("user", &user)
		c.Set("user_id", user.ID)
		c.Set("role", user.Role)
		c.Next()
	}
}

func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists || role != model.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Admin privilege required"})
			return
		}
		c.Next()
	}
}

func NodeTokenRequired(cm *cluster.ClusterManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Node-Token")
		if token == "" || !cm.ValidateToken(token) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid node cluster token"})
			return
		}
		c.Next()
	}
}
