package wechat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultBaseURL    = "https://ilinkai.weixin.qq.com"
	ChannelVersion    = "codex-plus-weixin/1.0"
	DefaultMaxChars   = 1800
	AbsoluteMaxChars  = 3800
	DefaultTimeoutSec = 35
)

// QRCodeResponse represents the result of fetching a login QR code.
type QRCodeResponse struct {
	Ret              int    `json:"ret"`
	ErrCode          int    `json:"errcode"`
	ErrMsg           string `json:"errmsg"`
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

// QRStatusResponse represents the scan status of a QR code.
type QRStatusResponse struct {
	Ret          int    `json:"ret"`
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
	Status       string `json:"status"` // wait, scaned, scaned_but_redirect, confirmed, expired
	BotToken     string `json:"bot_token"`
	ILinkBotID   string `json:"ilink_bot_id"`
	BaseURL      string `json:"baseurl"`
	ILinkUserID  string `json:"ilink_user_id"`
	RedirectHost string `json:"redirect_host"`
}

// TextItem represents a text payload in a WeChat message.
type TextItem struct {
	Text string `json:"text"`
}

// MessageItem represents an item within a WeChat message item_list.
type MessageItem struct {
	Type     int       `json:"type"` // 1: text, 2: image, 3: video, 5: file, 6: voice
	TextItem *TextItem `json:"text_item,omitempty"`
}

// InboundMessage represents a received WeChat message.
type InboundMessage struct {
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	ClientID     string        `json:"client_id"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state"`
	ItemList     []MessageItem `json:"item_list"`
	ContextToken string        `json:"context_token"`
	CreateTimeMs int64         `json:"create_time_ms"`
}

// UpdatesResponse represents the long polling updates response.
type UpdatesResponse struct {
	Ret                  int              `json:"ret"`
	ErrCode              int              `json:"errcode"`
	ErrMsg               string           `json:"errmsg"`
	Messages             []InboundMessage `json:"msgs"`
	GetUpdatesBuf        string           `json:"get_updates_buf"`
	LongPollingTimeoutMs int              `json:"longpolling_timeout_ms"`
}

// Client interacts with the WeChat iLink bot service.
type Client struct {
	baseURL    string
	token      string
	routeTag   string
	httpClient *http.Client
}

// NewClient creates a new WeChat iLink client.
func NewClient(baseURL, token, routeTag string, httpClient *http.Client) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		baseURL:    base,
		token:      strings.TrimSpace(token),
		routeTag:   strings.TrimSpace(routeTag),
		httpClient: httpClient,
	}
}

// SetBaseURL updates the client's base URL (e.g. after redirect).
func (c *Client) SetBaseURL(baseURL string) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base != "" {
		c.baseURL = base
	}
}

// SetToken updates the client's bot token.
func (c *Client) SetToken(token string) {
	c.token = strings.TrimSpace(token)
}

// GetQRCode fetches a QR code for user login.
func (c *Client) GetQRCode(ctx context.Context, botType string) (*QRCodeResponse, error) {
	if botType == "" {
		botType = "3"
	}
	endpoint := fmt.Sprintf("%s/ilink/bot/get_bot_qrcode?bot_type=%s", c.baseURL, url.QueryEscape(botType))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create get_bot_qrcode request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute get_bot_qrcode: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read get_bot_qrcode response: %w", err)
	}

	var res QRCodeResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("parse get_bot_qrcode response (%s): %w", string(bodyBytes), err)
	}
	return &res, nil
}

// GetQRStatus checks the status of the scanned QR code.
func (c *Client) GetQRStatus(ctx context.Context, qrcode string) (*QRStatusResponse, error) {
	endpoint := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", c.baseURL, url.QueryEscape(qrcode))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create get_qrcode_status request: %w", err)
	}
	req.Header.Set("ilink-app-clientversion", "1")
	if c.routeTag != "" {
		req.Header.Set("skroutetag", c.routeTag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute get_qrcode_status: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read get_qrcode_status response: %w", err)
	}

	var res QRStatusResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("parse get_qrcode_status response (%s): %w", string(bodyBytes), err)
	}
	return &res, nil
}

// GetUpdates polls the iLink service for new inbound messages.
func (c *Client) GetUpdates(ctx context.Context, getUpdatesBuf string, timeoutMs int) (*UpdatesResponse, error) {
	if timeoutMs < 1000 {
		timeoutMs = 1000
	} else if timeoutMs > 60000 {
		timeoutMs = 60000
	}

	payload := map[string]any{
		"get_updates_buf": getUpdatesBuf,
		"base_info": map[string]string{
			"channel_version": ChannelVersion,
		},
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal getupdates body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/ilink/bot/getupdates", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("create getupdates request: %w", err)
	}

	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read getupdates response: %w", err)
	}

	var res UpdatesResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("parse getupdates response (%s): %w", string(bodyBytes), err)
	}
	return &res, nil
}

// SendMessage sends a text reply back to the WeChat user, chunking if necessary.
func (c *Client) SendMessage(ctx context.Context, toUserID, text, contextToken string, maxReplyChars int) error {
	if maxReplyChars <= 0 {
		maxReplyChars = DefaultMaxChars
	}
	if maxReplyChars > AbsoluteMaxChars {
		maxReplyChars = AbsoluteMaxChars
	}

	chunks := ChunkText(text, maxReplyChars)
	if len(chunks) == 0 {
		chunks = []string{""}
	}

	endpoint := fmt.Sprintf("%s/ilink/bot/sendmessage", c.baseURL)

	for _, chunk := range chunks {
		clientID := uuid.New().String()
		payload := map[string]any{
			"msg": map[string]any{
				"from_user_id":   "",
				"to_user_id":     toUserID,
				"client_id":      clientID,
				"message_type":   2,
				"message_state":  2,
				"item_list": []map[string]any{
					{
						"type": 1,
						"text_item": map[string]string{
							"text": chunk,
						},
					},
				},
				"context_token":  contextToken,
			},
			"base_info": map[string]string{
				"channel_version": ChannelVersion,
			},
		}

		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal sendmessage body: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBytes))
		if err != nil {
			return fmt.Errorf("create sendmessage request: %w", err)
		}

		c.setAuthHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("sendmessage request failed: %w", err)
		}
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("sendmessage HTTP %d: %s", resp.StatusCode, string(respBytes))
		}
	}
	return nil
}

// SendTyping sends a typing indication for the given context.
func (c *Client) SendTyping(ctx context.Context, toUserID, contextToken string) error {
	endpoint := fmt.Sprintf("%s/ilink/bot/sendtyping", c.baseURL)
	payload := map[string]any{
		"to_user_id":    toUserID,
		"context_token": contextToken,
		"base_info": map[string]string{
			"channel_version": ChannelVersion,
		},
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorizationtype", "ilink_bot_token")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	req.Header.Set("x-wechat-uin", randomWeChatUIN())
	if c.routeTag != "" {
		req.Header.Set("skroutetag", c.routeTag)
	}
}

// ChunkText splits a string into chunks of at most maxChars unicode runes.
func ChunkText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = DefaultMaxChars
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	var chunks []string
	for len(runes) > maxChars {
		chunks = append(chunks, string(runes[:maxChars]))
		runes = runes[maxChars:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

func randomWeChatUIN() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	num := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(num), 10)))
}
