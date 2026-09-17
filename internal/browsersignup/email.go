package browsersignup

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"reg_go/internal/core"
	"reg_go/internal/email"
	"reg_go/internal/storage"
)

func applyEmailProvider(cfg *core.Config, provider string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	cfg.EmailProvider = provider
	switch provider {
	case "moemail":
		cfg.UseMoeMail = true
	case "cloudmail":
		cfg.UseCloudMail = true
	case "mailnest":
		cfg.UseMailNest = true
	case "mailalias":
		cfg.UseMailAlias = true
	case "icloud":
		cfg.UseICloud = true
	default:
		cfg.UseOutlook = true
		cfg.EmailProvider = "outlook"
	}
}

func attachEmail(ctx context.Context, cfg *core.Config, req StartRequest, index int) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(req.EmailProvider))
	if provider == "" {
		provider = "outlook"
	}
	emailProxy := cfg.EmailProxy
	switch provider {
	case "mailalias":
		config := req.MailAliasConfig
		if !email.MailAliasConfigReady(config) {
			config = email.GetMailAliasConfig()
		}
		if !email.MailAliasConfigReady(config) {
			return "", fmt.Errorf("请先配置 Gmail 临时邮箱")
		}
		provider := email.NewMailAliasProviderContextWithProxy(ctx, config, emailProxy)
		address, err := provider.GetAddress()
		if err != nil {
			return "", fmt.Errorf("生成 Gmail 临时邮箱失败: %w", err)
		}
		cfg.UseMailAlias = true
		cfg.MailAliasProvider = provider
		cfgCopy := config
		cfg.MailAliasConfig = &cfgCopy
		return address, nil

	case "mailnest":
		config := req.MailNestConfig
		if config == (email.MailNestConfig{}) {
			config = email.GetMailNestConfig()
		}
		if config == (email.MailNestConfig{}) {
			return "", fmt.Errorf("请先配置 MailNest")
		}
		p := email.NewMailNestProviderContextWithProxy(ctx, config, emailProxy)
		address, err := p.GetAddress()
		if err != nil {
			return "", fmt.Errorf("生成 MailNest 邮箱失败: %w", err)
		}
		cfg.UseMailNest = true
		cfg.MailNestProvider = p
		cfgCopy := config
		cfg.MailNestConfig = &cfgCopy
		return address, nil

	case "moemail":
		config, domain, err := pickMoeMail(req)
		if err != nil {
			return "", err
		}
		name := email.GenerateEmailName(index)
		settings := storage.GetAppSettings()
		expiry := int64(settings.MoeMailExpiryMinutes) * 60 * 1000
		if expiry <= 0 {
			expiry = 3600000
		}
		p, err := email.NewMoeMailProviderContextWithProxy(ctx, config, name, expiry, domain, emailProxy)
		if err != nil {
			return "", fmt.Errorf("生成 MoeMail 邮箱失败: %w", err)
		}
		cfg.UseMoeMail = true
		cfg.MoeMailProvider = p
		cfgCopy := config
		cfg.MoeMailConfig = &cfgCopy
		return p.GetAddress(), nil

	case "cloudmail":
		config, domain, err := pickCloudMail(req)
		if err != nil {
			return "", err
		}
		name := email.GenerateEmailName(index)
		p, err := email.NewCloudMailProviderContextWithProxy(ctx, config, name, domain, emailProxy)
		if err != nil {
			return "", fmt.Errorf("生成 cloud-mail 邮箱失败: %w", err)
		}
		cfg.UseCloudMail = true
		cfg.CloudMailProvider = p
		cfgCopy := config
		cfg.CloudMailConfig = &cfgCopy
		return p.GetAddress(), nil

	case "icloud":
		acc, err := nextICloud()
		if err != nil {
			return "", err
		}
		cfg.UseICloud = true
		cfg.ICloudAccount = &acc
		return acc.Email, nil

	default:
		acc, err := nextOutlook()
		if err != nil {
			return "", err
		}
		cfg.UseOutlook = true
		cfg.OutlookAccount = &acc
		return acc.Email, nil
	}
}

func pickMoeMail(req StartRequest) (email.MoeMailConfig, string, error) {
	if len(req.MoeMailDomains) > 0 && len(req.MoeMailConfigs) > 0 {
		var domain string
		if req.MoeMailRandomMode {
			domain = req.MoeMailDomains[rand.Intn(len(req.MoeMailDomains))]
		} else {
			domain = req.MoeMailDomains[0]
		}
		configs := req.MoeMailConfigs[domain]
		if len(configs) == 0 {
			return email.MoeMailConfig{}, "", fmt.Errorf("MoeMail 配置缺失")
		}
		return configs[rand.Intn(len(configs))], domain, nil
	}
	configs := email.GetMoeMailConfigs()
	if len(configs) == 0 {
		return email.MoeMailConfig{}, "", fmt.Errorf("请先在邮箱池配置 MoeMail")
	}
	client := email.NewMoeMailClient(configs[0])
	sys, err := client.GetSystemConfig()
	if err != nil {
		return email.MoeMailConfig{}, "", fmt.Errorf("获取 MoeMail 域名失败: %w", err)
	}
	if len(sys.Domains) == 0 {
		return email.MoeMailConfig{}, "", fmt.Errorf("MoeMail 没有可用域名")
	}
	return configs[0], sys.Domains[0], nil
}

func pickCloudMail(req StartRequest) (email.CloudMailConfig, string, error) {
	if len(req.CloudMailDomains) > 0 && len(req.CloudMailConfigs) > 0 {
		var domain string
		if req.CloudMailRandomMode {
			domain = req.CloudMailDomains[rand.Intn(len(req.CloudMailDomains))]
		} else {
			domain = req.CloudMailDomains[0]
		}
		configs := req.CloudMailConfigs[domain]
		if len(configs) == 0 {
			return email.CloudMailConfig{}, "", fmt.Errorf("cloud-mail 配置缺失")
		}
		return configs[rand.Intn(len(configs))], domain, nil
	}
	configs := email.GetCloudMailConfigs()
	if len(configs) == 0 {
		return email.CloudMailConfig{}, "", fmt.Errorf("请先在邮箱池配置 Cloud-Mail")
	}
	if len(configs[0].Domains) == 0 {
		return email.CloudMailConfig{}, "", fmt.Errorf("Cloud-Mail 没有可用域名")
	}
	return configs[0], configs[0].Domains[0], nil
}

func nextOutlook() (email.OutlookAccount, error) {
	stored := storage.GetAccountsCached()
	for _, acc := range stored {
		registered, _ := acc["registered"].(bool)
		if registered {
			continue
		}
		em, _ := acc["email"].(string)
		if em == "" {
			continue
		}
		password, _ := acc["password"].(string)
		clientID, _ := acc["clientId"].(string)
		refreshToken, _ := acc["refreshToken"].(string)
		mode, _ := acc["mode"].(string)
		return email.OutlookAccount{
			Email:        em,
			Password:     password,
			ClientID:     clientID,
			RefreshToken: refreshToken,
			Mode:         mode,
		}, nil
	}
	return email.OutlookAccount{}, fmt.Errorf("没有可用的 Outlook 账号")
}

func nextICloud() (email.ICloudAccount, error) {
	stored := email.GetICloudAccounts()
	for _, acc := range stored {
		registered, _ := acc["registered"].(bool)
		if registered {
			continue
		}
		em, _ := acc["email"].(string)
		murl, _ := acc["messagesURL"].(string)
		if em != "" && murl != "" {
			return email.ICloudAccount{Email: em, MessagesURL: murl}, nil
		}
	}
	return email.ICloudAccount{}, fmt.Errorf("没有可用的 iCloud 账号")
}
