package wechat

import (
	"context"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// Manager coordinates the lifecycle of WeChat integration.
type Manager struct {
	mu      sync.RWMutex
	service *Service
	client  *Client
}

// NewManager creates a WeChat Manager.
func NewManager(cfg config.WeChatConfig, apiURL, apiKey string) *Manager {
	client := NewClient(cfg.BaseURL, cfg.Token, cfg.RouteTag, nil)
	service := NewService(cfg, apiURL, apiKey)
	return &Manager{
		service: service,
		client:  client,
	}
}

// Service returns the underlying service.
func (m *Manager) Service() *Service {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.service
}

// GetQRCode fetches a login QR code from iLink.
func (m *Manager) GetQRCode(ctx context.Context, botType string) (*QRCodeResponse, error) {
	return m.client.GetQRCode(ctx, botType)
}

// GetQRStatus checks whether the user has confirmed QR login.
func (m *Manager) GetQRStatus(ctx context.Context, qrcode string) (*QRStatusResponse, error) {
	return m.client.GetQRStatus(ctx, qrcode)
}

// Status returns runtime status from the service.
func (m *Manager) Status() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.service == nil {
		return map[string]any{"running": false, "enabled": false}
	}
	return m.service.Status()
}

// Start boots the background polling service.
func (m *Manager) Start() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.service != nil {
		return m.service.Start()
	}
	return nil
}

// Stop shuts down the background polling service.
func (m *Manager) Stop() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.service != nil {
		m.service.Stop()
	}
}

// UpdateConfig updates config in the underlying service and client.
func (m *Manager) UpdateConfig(cfg config.WeChatConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.service != nil {
		m.service.UpdateConfig(cfg)
	}
	if m.client != nil {
		m.client.SetBaseURL(cfg.BaseURL)
		m.client.SetToken(cfg.Token)
	}
}

// GetConfig returns the current configuration.
func (m *Manager) GetConfig() config.WeChatConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.service != nil {
		return m.service.GetConfig()
	}
	return config.WeChatConfig{}
}
