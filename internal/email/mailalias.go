package email

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultMailAliasBaseURL = "http://43.165.0.86:63888"
	defaultMailAliasMode    = "mixed"
	defaultMailAliasLength  = 8
	minMailAliasLength      = 4
	maxMailAliasLength      = 24
	maxMailAliasPrefixLen   = 32
)

// MailAliasConfig Gmail 临时邮箱（mailalias-app）配置
type MailAliasConfig struct {
	BaseURL string `json:"baseUrl"`
	Prefix  string `json:"prefix"`
	Mode    string `json:"mode"`
	Length  int    `json:"length"`
}

type MailAliasClient struct {
	ctx    context.Context
	config MailAliasConfig
	client *http.Client
}

type MailAliasProvider struct {
	client  *MailAliasClient
	address string
}

type mailAliasCreateResponse struct {
	Alias  string  `json:"alias"`
	Mode   string  `json:"mode"`
	Length int     `json:"length"`
	Prefix *string `json:"prefix"`
	Error  string  `json:"error"`
}

type mailAliasMessage struct {
	ID  string `json:"id"`
	OTP string `json:"otp"`
}

type mailAliasMessagesResponse struct {
	Alias     string             `json:"alias"`
	Count     int                `json:"count"`
	LatestOTP string             `json:"latestOtp"`
	Messages  []mailAliasMessage `json:"messages"`
	Error     string             `json:"error"`
}

func normalizeMailAliasConfig(config MailAliasConfig) MailAliasConfig {
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.Prefix = sanitizeMailAliasPrefix(config.Prefix)
	switch strings.ToLower(strings.TrimSpace(config.Mode)) {
	case "lower", "upper", "digits", "name", "word", "mixed":
		config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	default:
		config.Mode = defaultMailAliasMode
	}
	if config.Length < minMailAliasLength || config.Length > maxMailAliasLength {
		config.Length = defaultMailAliasLength
	}
	return config
}

func sanitizeMailAliasPrefix(prefix string) string {
	var b strings.Builder
	for _, r := range prefix {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() >= maxMailAliasPrefixLen {
			break
		}
	}
	return b.String()
}

func NewMailAliasClient(config MailAliasConfig) *MailAliasClient {
	return newMailAliasClient(context.Background(), config)
}

func newMailAliasClient(ctx context.Context, config MailAliasConfig) *MailAliasClient {
	return &MailAliasClient{
		ctx:    ctx,
		config: normalizeMailAliasConfig(config),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func NewMailAliasProvider(config MailAliasConfig) *MailAliasProvider {
	return NewMailAliasProviderContext(context.Background(), config)
}

func NewMailAliasProviderContext(ctx context.Context, config MailAliasConfig) *MailAliasProvider {
	return NewMailAliasProviderContextWithProxy(ctx, config, "")
}

func NewMailAliasProviderContextWithProxy(ctx context.Context, config MailAliasConfig, proxyURL string) *MailAliasProvider {
	client := newMailAliasClient(ctx, config)
	if proxyURL != "" {
		client.client = httpClientWithProxy(proxyURL, 15*time.Second)
	}
	return &MailAliasProvider{client: client}
}

func (c *MailAliasClient) request(method, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(c.ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return c.client.Do(req)
}

func (c *MailAliasClient) CreateAlias() (string, error) {
	if c.config.BaseURL == "" {
		return "", fmt.Errorf("未配置 MailAlias API URL")
	}
	q := url.Values{}
	q.Set("mode", c.config.Mode)
	q.Set("length", fmt.Sprintf("%d", c.config.Length))
	if c.config.Prefix != "" {
		q.Set("prefix", c.config.Prefix)
	}
	resp, err := c.request("GET", c.config.BaseURL+"/api/alias?"+q.Encode())
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	var data mailAliasCreateResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("解析响应失败: %w, 响应内容: %s", err, string(body))
	}
	if resp.StatusCode != 200 {
		msg := data.Error
		if msg == "" {
			msg = string(body)
		}
		return "", fmt.Errorf("生成别名失败 %d: %s", resp.StatusCode, msg)
	}
	alias := strings.TrimSpace(data.Alias)
	if alias == "" {
		return "", fmt.Errorf("MailAlias 未返回邮箱地址")
	}
	return alias, nil
}

func (c *MailAliasClient) GetMessages(alias string) (*mailAliasMessagesResponse, error) {
	if c.config.BaseURL == "" {
		return nil, fmt.Errorf("未配置 MailAlias API URL")
	}
	q := url.Values{}
	q.Set("alias", alias)
	q.Set("limit", "15")
	resp, err := c.request("GET", c.config.BaseURL+"/api/messages?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var data mailAliasMessagesResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w, 响应内容: %s", err, string(body))
	}
	if resp.StatusCode != 200 {
		msg := data.Error
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("获取邮件失败 %d: %s", resp.StatusCode, msg)
	}
	return &data, nil
}

func (p *MailAliasProvider) GetAddress() (string, error) {
	if p == nil || p.client == nil {
		return "", fmt.Errorf("MailAliasProvider 未初始化")
	}
	if p.address != "" {
		return p.address, nil
	}
	alias, err := p.client.CreateAlias()
	if err != nil {
		return "", err
	}
	p.address = alias
	return p.address, nil
}

func (p *MailAliasProvider) WaitForCode(timeout, interval int) (string, error) {
	if p == nil || p.client == nil {
		return "", fmt.Errorf("MailAliasProvider 未初始化")
	}
	ctx := p.client.ctx
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if p.address == "" {
		if _, err := p.GetAddress(); err != nil {
			return "", err
		}
	}
	if interval <= 0 {
		interval = 3
	}
	if timeout <= 0 {
		timeout = interval
	}
	maxRetries := timeout / interval
	if maxRetries < 1 {
		maxRetries = 1
	}
	log.Printf("[MailAlias] 开始等待验证码 %s", p.address)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		mails, err := p.client.GetMessages(p.address)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			if attempt%5 == 0 {
				log.Printf("[MailAlias] 获取邮件失败: %v，重试中...", err)
			}
			if err := waitEmailPoll(ctx, time.Duration(interval)*time.Second); err != nil {
				return "", err
			}
			continue
		}
		if code := pickMailAliasOTP(mails); code != "" {
			log.Printf("[MailAlias] 从新邮件中获取到验证码: %s", code)
			return code, nil
		}
		if attempt%5 == 0 {
			log.Printf("[MailAlias] [%d/%d] 暂无新邮件...", attempt, maxRetries)
		}
		if err := waitEmailPoll(ctx, time.Duration(interval)*time.Second); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("等待验证码超时 (%ds)", timeout)
}

func pickMailAliasOTP(resp *mailAliasMessagesResponse) string {
	if resp == nil {
		return ""
	}
	if code := normalizeMailAliasOTP(resp.LatestOTP); code != "" {
		return code
	}
	for _, m := range resp.Messages {
		if code := normalizeMailAliasOTP(m.OTP); code != "" {
			return code
		}
	}
	return ""
}

func normalizeMailAliasOTP(code string) string {
	code = strings.TrimSpace(code)
	if len(code) < 4 || len(code) > 8 {
		return ""
	}
	hasDigit := false
	for _, r := range code {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return ""
	}
	return code
}
