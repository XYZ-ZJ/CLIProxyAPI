package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	log "github.com/sirupsen/logrus"
)

// ChatMessage represents a single message in conversation history.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// UserSession maintains recent conversation context for a user.
type UserSession struct {
	mu           sync.Mutex
	Model        string
	Messages     []ChatMessage
	LastActivity time.Time
}

// Service manages the background long-polling and message handling loop.
type Service struct {
	cfg        config.WeChatConfig
	cfgMu      sync.RWMutex
	client     *Client
	apiURL     string
	apiKey     string
	httpClient *http.Client

	ctx        context.Context
	cancel     context.CancelFunc
	running    atomic.Bool

	seenMessages sync.Map // map[string]time.Time
	sessions     sync.Map // map[string]*UserSession

	lastActiveTime atomic.Int64 // Unix timestamp
	totalProcessed atomic.Int64
}

// NewService creates a WeChat direct connection service.
func NewService(cfg config.WeChatConfig, apiURL, apiKey string) *Service {
	client := NewClient(cfg.BaseURL, cfg.Token, cfg.RouteTag, nil)
	return &Service{
		cfg:        cfg,
		client:     client,
		apiURL:     apiURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}
}

// IsRunning reports whether the service is actively polling.
func (s *Service) IsRunning() bool {
	return s.running.Load()
}

// GetConfig returns a copy of current config.
func (s *Service) GetConfig() config.WeChatConfig {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// UpdateConfig updates service configuration and refreshes client credentials.
func (s *Service) UpdateConfig(newCfg config.WeChatConfig) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.cfg = newCfg
	s.client.SetBaseURL(newCfg.BaseURL)
	s.client.SetToken(newCfg.Token)
}

// SetAPIURL updates the local backend API URL.
func (s *Service) SetAPIURL(apiURL string) {
	s.apiURL = apiURL
}

// Start begins the long polling background loop.
func (s *Service) Start() error {
	if s.running.Swap(true) {
		return nil // already running
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	log.Info("wechat: starting direct connection service")
	go s.pollLoop()
	return nil
}

// Stop gracefully shuts down the polling loop.
func (s *Service) Stop() {
	if !s.running.Swap(false) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	log.Info("wechat: stopped direct connection service")
}

// Status returns a runtime status snapshot.
func (s *Service) Status() map[string]any {
	cfg := s.GetConfig()
	lastActive := s.lastActiveTime.Load()
	var lastActiveStr string
	if lastActive > 0 {
		lastActiveStr = time.Unix(lastActive, 0).Format(time.RFC3339)
	}

	return map[string]any{
		"running":           s.IsRunning(),
		"enabled":           cfg.Enabled,
		"base_url":          cfg.BaseURL,
		"account_id":        cfg.AccountID,
		"allow_all":         cfg.AllowAll,
		"allow_users_count": len(cfg.AllowUserIDs),
		"default_model":     cfg.DefaultModel,
		"total_processed":   s.totalProcessed.Load(),
		"last_active":       lastActiveStr,
	}
}

func (s *Service) pollLoop() {
	var syncBuf string
	failures := 0

	for s.running.Load() {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		cfg := s.GetConfig()
		if cfg.Token == "" {
			time.Sleep(3 * time.Second)
			continue
		}

		pollTimeout := cfg.PollTimeoutSeconds
		if pollTimeout <= 0 {
			pollTimeout = DefaultTimeoutSec
		}

		resp, err := s.client.GetUpdates(s.ctx, syncBuf, pollTimeout*1000)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			failures++
			log.Warnf("wechat: poll error (%d consecutive): %v", failures, err)
			backoff := 2 * time.Second
			if failures >= 3 {
				backoff = 10 * time.Second
			}
			time.Sleep(backoff)
			continue
		}

		failures = 0
		s.lastActiveTime.Store(time.Now().Unix())

		if resp.GetUpdatesBuf != "" {
			syncBuf = resp.GetUpdatesBuf
		}

		for _, msg := range resp.Messages {
			go s.handleMessage(msg)
		}
	}
}

func (s *Service) handleMessage(msg InboundMessage) {
	msgKey := msg.ClientID
	if msgKey == "" {
		msgKey = fmt.Sprintf("%s_%d", msg.FromUserID, msg.CreateTimeMs)
	}
	if msgKey != "" {
		if _, loaded := s.seenMessages.LoadOrStore(msgKey, time.Now()); loaded {
			return
		}
	}

	// Extract incoming text
	var textContent string
	for _, item := range msg.ItemList {
		if item.Type == 1 && item.TextItem != nil {
			textContent += item.TextItem.Text
		}
	}
	rawText := strings.TrimSpace(textContent)
	if rawText == "" {
		return
	}

	fromUser := msg.FromUserID
	contextToken := msg.ContextToken
	cfg := s.GetConfig()

	// Special command: /id
	if rawText == "/id" {
		reply := fmt.Sprintf("您的微信 iLink User ID 是:\n%s", fromUser)
		_ = s.client.SendMessage(s.ctx, fromUser, reply, contextToken, cfg.MaxReplyChars)
		return
	}

	// Special command: /help
	if rawText == "/help" {
		helpText := "【Codex 微信助手帮助】\n" +
			"- 直接发送文字即可开始对话\n" +
			"- 发送 /id 查看自己的微信识别码\n" +
			"- 发送 /clear 清除当前对话上下文\n" +
			"- 发送 /model <模型名> 切换使用模型"
		_ = s.client.SendMessage(s.ctx, fromUser, helpText, contextToken, cfg.MaxReplyChars)
		return
	}

	// Session management
	session := s.getUserSession(fromUser, cfg.DefaultModel)

	// Special command: /clear
	if rawText == "/clear" {
		session.mu.Lock()
		session.Messages = nil
		session.mu.Unlock()
		_ = s.client.SendMessage(s.ctx, fromUser, "已清空对话上下文记录。", contextToken, cfg.MaxReplyChars)
		return
	}

	// Special command: /model
	if strings.HasPrefix(rawText, "/model") {
		parts := strings.Fields(rawText)
		if len(parts) >= 2 {
			newModel := parts[1]
			session.mu.Lock()
			session.Model = newModel
			session.mu.Unlock()
			reply := fmt.Sprintf("已切换当前对话模型为：%s", newModel)
			_ = s.client.SendMessage(s.ctx, fromUser, reply, contextToken, cfg.MaxReplyChars)
		} else {
			session.mu.Lock()
			curModel := session.Model
			session.mu.Unlock()
			reply := fmt.Sprintf("当前使用模型：%s\n使用 /model <模型名> 可更换模型。", curModel)
			_ = s.client.SendMessage(s.ctx, fromUser, reply, contextToken, cfg.MaxReplyChars)
		}
		return
	}

	// Authorization check
	if !cfg.AllowAll {
		authorized := false
		for _, uid := range cfg.AllowUserIDs {
			if strings.TrimSpace(uid) == fromUser {
				authorized = true
				break
			}
		}
		if !authorized {
			denyText := fmt.Sprintf("未授权用户。您的 ID 是:\n%s\n请联系管理员添加到白名单。", fromUser)
			_ = s.client.SendMessage(s.ctx, fromUser, denyText, contextToken, cfg.MaxReplyChars)
			return
		}
	}

	// Prefix check
	prompt := rawText
	if cfg.RequirePrefix {
		prefix := cfg.Prefix
		if prefix == "" {
			prefix = "/codex"
		}
		if !strings.HasPrefix(prompt, prefix) {
			return
		}
		prompt = strings.TrimSpace(strings.TrimPrefix(prompt, prefix))
		if prompt == "" {
			return
		}
	}

	// Indicate typing status
	_ = s.client.SendTyping(s.ctx, fromUser, contextToken)

	// Call LLM
	s.totalProcessed.Add(1)
	replyText, err := s.callLLM(session, prompt)
	if err != nil {
		log.Errorf("wechat: llm error for %s: %v", fromUser, err)
		errReply := fmt.Sprintf("处理请求失败：%v", err)
		_ = s.client.SendMessage(s.ctx, fromUser, errReply, contextToken, cfg.MaxReplyChars)
		return
	}

	if strings.TrimSpace(replyText) == "" {
		replyText = "（模型无响应内容）"
	}

	// Send result
	_ = s.client.SendMessage(s.ctx, fromUser, replyText, contextToken, cfg.MaxReplyChars)
}

func (s *Service) getUserSession(userID, defaultModel string) *UserSession {
	val, ok := s.sessions.Load(userID)
	if ok {
		return val.(*UserSession)
	}
	model := defaultModel
	if model == "" {
		model = "gpt-4o"
	}
	newSession := &UserSession{
		Model:        model,
		Messages:     make([]ChatMessage, 0, 10),
		LastActivity: time.Now(),
	}
	actual, _ := s.sessions.LoadOrStore(userID, newSession)
	return actual.(*UserSession)
}

func (s *Service) callLLM(session *UserSession, userPrompt string) (string, error) {
	session.mu.Lock()
	session.Messages = append(session.Messages, ChatMessage{Role: "user", Content: userPrompt})
	// keep history bounded to 12 messages
	if len(session.Messages) > 12 {
		session.Messages = session.Messages[len(session.Messages)-12:]
	}
	model := session.Model
	msgsCopy := make([]ChatMessage, len(session.Messages))
	copy(msgsCopy, session.Messages)
	session.mu.Unlock()

	reqBody := map[string]any{
		"model":    model,
		"messages": msgsCopy,
		"stream":   false,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := s.apiURL
	if url == "" {
		url = "http://127.0.0.1:8317/v1/chat/completions"
	}

	httpReq, err := http.NewRequestWithContext(s.ctx, http.MethodPost, url, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("parse completion: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty choices from completion")
	}

	assistantReply := parsed.Choices[0].Message.Content

	session.mu.Lock()
	session.Messages = append(session.Messages, ChatMessage{Role: "assistant", Content: assistantReply})
	session.LastActivity = time.Now()
	session.mu.Unlock()

	return assistantReply, nil
}
