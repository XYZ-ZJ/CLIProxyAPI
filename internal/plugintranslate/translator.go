package plugintranslate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Translation contains the localized name and description for a plugin.
type Translation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Translator handles plugin metadata localization with offline dictionary and LLM translation fallback.
type Translator struct {
	mu          sync.RWMutex
	cache       map[string]Translation
	cacheFile   string
	apiURL      string
	httpClient  *http.Client
	pending     sync.Map // deduplicate in-flight translations
	builtinDict map[string]Translation
}

var (
	defaultTranslator *Translator
	once              sync.Once
)

// GetDefault returns the global Translator instance.
func GetDefault() *Translator {
	once.Do(func() {
		defaultTranslator = NewTranslator("", "plugins-translation-cache.json")
	})
	return defaultTranslator
}

// NewTranslator creates a new plugin translator.
func NewTranslator(apiURL, cacheFile string) *Translator {
	if cacheFile == "" {
		cacheFile = "plugins-translation-cache.json"
	}
	t := &Translator{
		cache:       make(map[string]Translation),
		cacheFile:   cacheFile,
		apiURL:      apiURL,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		builtinDict: initBuiltinDict(),
	}
	t.loadCache()
	return t
}

// SetAPIURL updates the local LLM endpoint URL.
func (t *Translator) SetAPIURL(apiURL string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.apiURL = apiURL
}

func initBuiltinDict() map[string]Translation {
	return map[string]Translation{
		"jshandler": {
			Name:        "JS 脚本拦截器",
			Description: "使用 Goja 虚拟机引擎执行外部 JavaScript 脚本，拦截和修改请求、响应及流式分块。",
		},
		"gemini-cli": {
			Name:        "Gemini CLI 渠道接入",
			Description: "通过原生插件 ABI 为 CLIProxyAPI 添加 Gemini CLI 上游模型渠道提供商支持。",
		},
		"codex-invite": {
			Name:        "Codex 邀请直发助手",
			Description: "使用指定的 Codex OAuth 凭证自动从 CPA 发起并发送 Codex 推荐邀请邮件。",
		},
		"privacyfilter": {
			Name:        "隐私与机密脱敏过滤",
			Description: "使用嵌入式 Gitleaks 规则拦截模型请求，在上游前自动脱敏邮箱、电话、API Key、Token 等机密信息。",
		},
		"antigravity-coding-filter": {
			Name:        "Antigravity 防封安全过滤",
			Description: "拦截编程工具的敏感请求，特别是针对 Google Antigravity 的逆向流量，有效防止账号被风控封禁。",
		},
		"priority-auto-router": {
			Name:        "高优先级智能自动路由",
			Description: "将 Claude Code 与 Codex CLI 的模型别名请求按照配置的优先级渠道分发，支持故障自动切换。",
		},
		"codex-429-autoban": {
			Name:        "Codex 429 智能防限流调度",
			Description: "在触发 429 限流时自动将对应凭证放入冷却队列，并在 5 小时或每周窗口刷新后自动唤醒恢复。",
		},
		"model-fallback-router": {
			Name:        "模型故障转移降级路由",
			Description: "当主模型因额度耗尽、速率限制或网络错误请求失败时，自动无缝重试并切换到配置的备选模型。",
		},
		"embeddings-forward": {
			Name:        "向量嵌入与 Rerank 转发",
			Description: "转发 OpenAI 兼容的 Embeddings 与 Cohere 风格的重排 (Rerank) 请求，支持多模型别名与故障转移。",
		},
		"daybreak-blue": {
			Name:        "Daybreak 模型扩展",
			Description: "向 CLIProxyAPI 的 Codex 模型目录中添加 Daybreak Blue 与 Daybreak Red 系列大模型。",
		},
		"model-list-tweaker": {
			Name:        "模型列表精简与重命名",
			Description: "在全局模型目录中重新排序、重命名显示名称、显示剩余额度百分比及隐藏特定模型，内置可视化编辑器。",
		},
		"opencode-go-oauth": {
			Name:        "OpenCode Go 认证扩展",
			Description: "提供 OpenCode Go / Go Plus OAuth 设备扫码登录，支持 44 款大模型跨协议调用与额度统计。",
		},
		"imagegen": {
			Name:        "AI 图像与位图生成",
			Description: "使用 AI 模型生成或修改位图图像，支持开发原型设计、贴图生成、材质与透明抠图。",
		},
		"pdf": {
			Name:        "PDF 专业文档处理套件",
			Description: "精准读取、创建、分析、渲染并验证 PDF 文件的排版布局，支持表格与交互式表单提取。",
		},
		"spreadsheets": {
			Name:        "电子表格分析处理套件",
			Description: "创建、修改、分析并可视化 Excel (.xlsx) 与 CSV 表格，支持高级公式计算、图表与样式排版。",
		},
		"presentations": {
			Name:        "演示文稿 PPT 制作套件",
			Description: "阅读、创建或编辑 PowerPoint (PPT/PPTX) 幻灯片，支持幻灯片排版与自动化生成。",
		},
		"reverse-engineer-anything": {
			Name:        "REA 全能逆向工程分析",
			Description: "逆向分析并反编译原生可执行程序、托管二进制代码、Electron 客户端与浏览器应用源码。",
		},
		"codex-security": {
			Name:        "Codex 深度安全审计套件",
			Description: "代码仓库级安全漏洞扫描、攻击路径溯源、安全加固建议与补丁风险综合评估。",
		},
		"find-docs": {
			Name:        "开发者技术文档即时检索",
			Description: "即时抓取并检索任意流行开发库、框架和 SDK 的最新官方文档、API 参考与实用代码示例。",
		},
		"design-taste-frontend": {
			Name:        "现代前端审美设计指南",
			Description: "提炼自 247 个国际顶尖设计获奖网站的前端视觉排版、色彩方案与无废话专业 UI 风格指南。",
		},
	}
}

func (t *Translator) loadCache() {
	if t.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(t.cacheFile)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = json.Unmarshal(data, &t.cache)
}

func (t *Translator) saveCache() {
	if t.cacheFile == "" {
		return
	}
	t.mu.RLock()
	data, err := json.MarshalIndent(t.cache, "", "  ")
	t.mu.RUnlock()
	if err != nil {
		return
	}
	_ = os.WriteFile(t.cacheFile, data, 0644)
}

// Localize returns the translated name and description if available.
// If not translated yet, it returns the original texts and triggers an asynchronous background translation.
func (t *Translator) Localize(ctx context.Context, id, originalName, originalDesc string) (string, string) {
	id = strings.TrimSpace(id)
	// 1. Check built-in curated dictionary
	if builtin, ok := t.builtinDict[id]; ok {
		return builtin.Name, builtin.Description
	}

	// 2. Check persistent cache
	t.mu.RLock()
	cached, ok := t.cache[id]
	t.mu.RUnlock()
	if ok && cached.Name != "" && cached.Description != "" {
		return cached.Name, cached.Description
	}

	// 3. Trigger async model translation if text is predominantly English
	if containsEnglish(originalDesc) {
		go t.asyncTranslate(id, originalName, originalDesc)
	}

	return originalName, originalDesc
}

func (t *Translator) asyncTranslate(id, name, desc string) {
	if _, loaded := t.pending.LoadOrStore(id, true); loaded {
		return
	}
	defer t.pending.Delete(id)

	t.mu.RLock()
	apiURL := t.apiURL
	t.mu.RUnlock()

	if apiURL == "" {
		apiURL = "http://127.0.0.1:8317/v1/chat/completions"
	}

	prompt := fmt.Sprintf("请将以下开发工具/插件的英文名称和描述翻译为准确、清晰、专业的中文。\n名称: %s\n描述: %s\n\n请直接输出严格的 JSON 格式（不要包含任何 markdown 代码块或多余解释）：\n{\"name\": \"中文名称\", \"description\": \"中文描述\"}", name, desc)

	reqBody := map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, apiURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}

	var completionResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &completionResp); err != nil || len(completionResp.Choices) == 0 {
		return
	}

	rawResult := strings.TrimSpace(completionResp.Choices[0].Message.Content)
	rawResult = strings.TrimPrefix(rawResult, "```json")
	rawResult = strings.TrimPrefix(rawResult, "```")
	rawResult = strings.TrimSuffix(rawResult, "```")
	rawResult = strings.TrimSpace(rawResult)

	var res Translation
	if err := json.Unmarshal([]byte(rawResult), &res); err != nil {
		return
	}

	if res.Name != "" && res.Description != "" {
		t.mu.Lock()
		t.cache[id] = res
		t.mu.Unlock()
		t.saveCache()
		log.Infof("plugintranslate: translated plugin [%s] -> %s", id, res.Name)
	}
}

func containsEnglish(s string) bool {
	asciiCount := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			asciiCount++
		}
	}
	return asciiCount > 10
}
