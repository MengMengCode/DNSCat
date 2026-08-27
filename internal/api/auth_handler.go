package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"dnscat/internal/config"
	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	cfg *config.Config
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterReq struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid login parameters"})
		return
	}

	var user model.User
	if err := database.DB.Where("username = ? OR email = ?", req.Username, req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	token, err := GenerateJWT(&user, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"role":       user.Role,
			"api_key":    user.APIKey,
			"created_at": user.CreatedAt,
		},
	})
}

func (h *AuthHandler) Register(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{
		"error": "Public registration is disabled. Administrator credentials are created upon installation or managed via the 'dnscat' CLI command on the server.",
	})
}

func (h *AuthHandler) GetMe(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	u := user.(*model.User)
	c.JSON(http.StatusOK, gin.H{
		"id":         u.ID,
		"username":   u.Username,
		"email":      u.Email,
		"role":       u.Role,
		"api_key":    u.APIKey,
		"language":   u.Language,
		"theme":      u.Theme,
		"created_at": u.CreatedAt,
	})
}

// UpdatePreferencesReq 是界面偏好的更新载荷。两项都用指针，
// 以便只提交其中一项时不把另一项覆盖成空值。
type UpdatePreferencesReq struct {
	Language *string `json:"language"`
	Theme    *string `json:"theme"`
}

// UpdatePreferences 保存当前账号的界面语言与主题。
// 未登录状态下前端只写浏览器本地存储，不会调用这里；登录后以账号里的值为准，
// 使同一账号换浏览器登录也能保持一致的语言与主题。
func (h *AuthHandler) UpdatePreferences(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req UpdatePreferencesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid preference payload"})
		return
	}

	updates := make(map[string]interface{}, 2)
	// 白名单校验：只接受已知取值，避免把任意字符串写进库再回传给前端。
	if req.Language != nil {
		lang := strings.TrimSpace(*req.Language)
		if lang != "zh-CN" && lang != "en-US" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported language"})
			return
		}
		updates["language"] = lang
	}
	if req.Theme != nil {
		theme := strings.TrimSpace(*req.Theme)
		if theme != "dark" && theme != "light" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported theme"})
			return
		}
		updates["theme"] = theme
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nothing to update"})
		return
	}

	if err := database.DB.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save preferences"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Preferences saved"})
}

func (h *AuthHandler) RegenerateAPIKey(c *gin.Context) {
	userID := c.GetUint("user_id")
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	newKey := "dnscat_" + hex.EncodeToString(b)

	if err := database.DB.Model(&model.User{}).Where("id = ?", userID).Update("api_key", newKey).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"api_key": newKey})
}

type ChangePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ChangePassword 修改当前登录用户（管理员）的登录密码：校验原密码后写入新的 bcrypt 哈希。
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID := c.GetUint("user_id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req ChangePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少需要 6 位字符 (new password must be at least 6 characters)"})
		return
	}

	var user model.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前密码不正确 (current password is incorrect)"})
		return
	}

	if req.OldPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能与当前密码相同 (new password must differ from the current one)"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	if err := database.DB.Model(&model.User{}).Where("id = ?", userID).Update("password_hash", string(hash)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}
