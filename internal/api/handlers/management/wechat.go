package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/wechat"
)

// GetWeChatStatus returns the current runtime status of WeChat integration.
func (h *Handler) GetWeChatStatus(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "running": false})
		return
	}
	c.JSON(http.StatusOK, h.wechatManager.Status())
}

// GetWeChatQR fetches a new login QR code.
func (h *Handler) GetWeChatQR(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wechat integration not initialized"})
		return
	}
	botType := c.DefaultQuery("bot_type", "3")
	qrResp, err := h.wechatManager.GetQRCode(c.Request.Context(), botType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, qrResp)
}

// GetWeChatQRStatus checks whether the QR code has been scanned/confirmed.
func (h *Handler) GetWeChatQRStatus(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wechat integration not initialized"})
		return
	}
	qrcode := c.Query("qrcode")
	if qrcode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing qrcode parameter"})
		return
	}

	statusResp, err := h.wechatManager.GetQRStatus(c.Request.Context(), qrcode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// If confirmed, automatically persist token and start service
	if statusResp.Status == "confirmed" && statusResp.BotToken != "" {
		cfg := h.wechatManager.GetConfig()
		cfg.Token = statusResp.BotToken
		cfg.AccountID = statusResp.ILinkBotID
		if statusResp.BaseURL != "" {
			cfg.BaseURL = statusResp.BaseURL
		}
		cfg.Enabled = true
		h.wechatManager.UpdateConfig(cfg)
		_ = h.wechatManager.Start()
	}

	c.JSON(http.StatusOK, statusResp)
}

// PostWeChatStart starts the WeChat background long polling loop.
func (h *Handler) PostWeChatStart(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wechat integration not initialized"})
		return
	}
	if err := h.wechatManager.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "started", "running": true})
}

// PostWeChatStop stops the WeChat polling loop.
func (h *Handler) PostWeChatStop(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wechat integration not initialized"})
		return
	}
	h.wechatManager.Stop()
	c.JSON(http.StatusOK, gin.H{"status": "stopped", "running": false})
}

// GetWeChatConfig returns WeChat configuration.
func (h *Handler) GetWeChatConfig(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusOK, config.WeChatConfig{})
		return
	}
	c.JSON(http.StatusOK, h.wechatManager.GetConfig())
}

// PutWeChatConfig updates WeChat configuration.
func (h *Handler) PutWeChatConfig(c *gin.Context) {
	if h.wechatManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wechat integration not initialized"})
		return
	}
	var newCfg config.WeChatConfig
	if err := c.ShouldBindJSON(&newCfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.wechatManager.UpdateConfig(newCfg)
	if newCfg.Enabled && !h.wechatManager.Service().IsRunning() {
		_ = h.wechatManager.Start()
	} else if !newCfg.Enabled && h.wechatManager.Service().IsRunning() {
		h.wechatManager.Stop()
	}
	c.JSON(http.StatusOK, gin.H{"status": "updated", "config": newCfg})
}
