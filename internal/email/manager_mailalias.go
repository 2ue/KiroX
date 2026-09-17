package email

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"reg_go/internal/storage"
)

func getMailAliasConfigPath() string {
	return filepath.Join(storage.GetDataDir(), "mailalias.json")
}

func TestMailAliasConnection(configJSON string) map[string]interface{} {
	var config MailAliasConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return map[string]interface{}{"error": "配置格式错误: " + err.Error()}
	}
	config = normalizeMailAliasConfig(config)
	if config.BaseURL == "" {
		return map[string]interface{}{"error": "请填写 API URL"}
	}
	client := NewMailAliasClient(config)
	alias, err := client.CreateAlias()
	if err != nil {
		return map[string]interface{}{"error": "连接失败: " + err.Error()}
	}
	return map[string]interface{}{
		"success": true,
		"alias":   alias,
	}
}

func SaveMailAliasConfig(jsonData string) map[string]interface{} {
	var config MailAliasConfig
	if err := json.Unmarshal([]byte(jsonData), &config); err != nil {
		return map[string]interface{}{"error": "配置格式错误: " + err.Error()}
	}
	config = normalizeMailAliasConfig(config)
	if config.BaseURL == "" {
		return map[string]interface{}{"error": "请填写 API URL"}
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return map[string]interface{}{"error": "保存失败: " + err.Error()}
	}
	path := getMailAliasConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return map[string]interface{}{"error": "保存失败: " + err.Error()}
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return map[string]interface{}{"error": "保存失败: " + err.Error()}
	}
	log.Printf("[MailAlias] config saved")
	return map[string]interface{}{"success": true}
}

func GetMailAliasConfig() MailAliasConfig {
	data, err := os.ReadFile(getMailAliasConfigPath())
	if err != nil {
		return MailAliasConfig{}
	}
	var config MailAliasConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("[MailAlias] invalid config file, resetting: %v", err)
		_ = os.Remove(getMailAliasConfigPath())
		return MailAliasConfig{}
	}
	return normalizeMailAliasConfig(config)
}

func MailAliasConfigReady(config MailAliasConfig) bool {
	return strings.TrimSpace(config.BaseURL) != ""
}
