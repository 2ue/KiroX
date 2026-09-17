package browsersignup

import (
	"context"
	"fmt"
	"sync"
	"time"

	"reg_go/internal/email"
)

// StartRequest is the payload from the browser-signup tab.
type StartRequest struct {
	Engine              string                             `json:"engine"`
	Headless            bool                               `json:"headless"`
	Count               int                                `json:"count"`
	Proxy               string                             `json:"proxy"`
	ProxyConfigured     bool                               `json:"proxyConfigured"`
	EmailProvider       string                             `json:"emailProvider"`
	MailAliasConfig     email.MailAliasConfig              `json:"mailAliasConfig"`
	MailNestConfig      email.MailNestConfig               `json:"mailNestConfig"`
	MoeMailDomains      []string                           `json:"moemailDomains"`
	MoeMailConfigs      map[string][]email.MoeMailConfig   `json:"moemailConfigs"`
	MoeMailRandomMode   bool                               `json:"moemailRandomMode"`
	CloudMailDomains    []string                           `json:"cloudmailDomains"`
	CloudMailConfigs    map[string][]email.CloudMailConfig `json:"cloudmailConfigs"`
	CloudMailRandomMode bool                               `json:"cloudmailRandomMode"`
}

type manager struct {
	mu            sync.Mutex
	running       bool
	installing    bool
	installEngine string
	currentEngine string
	total         int
	completed     int
	success       int
	failed        int
	step          string
	startTime     time.Time
	logs          []string
	logsMu        sync.Mutex
	cancel        context.CancelFunc
}

var mgr = &manager{
	logs: make([]string, 0),
}

func (m *manager) appendLog(msg string) {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	stamp := time.Now().Format("15:04:05")
	line := stamp + " " + msg
	m.logs = append(m.logs, line)
	if len(m.logs) > 500 {
		m.logs = m.logs[len(m.logs)-500:]
	}
}

func (m *manager) clearLogs() {
	m.logsMu.Lock()
	m.logs = nil
	m.logsMu.Unlock()
}

// IsRunning reports whether a browser-signup batch is active.
func IsRunning() bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.running
}

// GetLogs returns a copy of browser-signup logs.
func GetLogs() []string {
	mgr.logsMu.Lock()
	defer mgr.logsMu.Unlock()
	out := make([]string, len(mgr.logs))
	copy(out, mgr.logs)
	return out
}

// GetStatus returns live counters for the browser-signup tab.
func GetStatus() map[string]interface{} {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	elapsed := 0.0
	if mgr.running && !mgr.startTime.IsZero() {
		elapsed = time.Since(mgr.startTime).Seconds()
	}
	return map[string]interface{}{
		"running":        mgr.running,
		"installing":     mgr.installing,
		"installEngine":  mgr.installEngine,
		"total":          mgr.total,
		"completed":      mgr.completed,
		"success":        mgr.success,
		"failed":         mgr.failed,
		"step":           mgr.step,
		"elapsed":        elapsed,
		"engine":         mgr.currentEngine,
	}
}

// EngineInfo returns Python / Camoufox / Playwright status.
func EngineInfo() EngineStatus {
	return snapshotEngineStatus()
}

// StartInstall begins a background pip install of camoufox or playwright.
func StartInstall(engine string) map[string]interface{} {
	engine = normalizeEngine(engine)
	mgr.mu.Lock()
	if mgr.running {
		mgr.mu.Unlock()
		return map[string]interface{}{"error": "浏览器注册任务正在运行"}
	}
	if mgr.installing {
		mgr.mu.Unlock()
		return map[string]interface{}{"error": "正在安装浏览器引擎"}
	}
	mgr.installing = true
	mgr.installEngine = engine
	mgr.mu.Unlock()
	mgr.appendLog("[引擎] 开始安装 " + engine)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err := installEngine(ctx, engine, func(msg string) {
			mgr.appendLog("[引擎] " + msg)
		})
		mgr.mu.Lock()
		mgr.installing = false
		mgr.mu.Unlock()
		if err != nil {
			mgr.appendLog("[引擎] 安装失败: " + err.Error())
			return
		}
		mgr.appendLog("[引擎] 安装完成")
	}()
	return map[string]interface{}{"status": "installing", "engine": engine}
}

// Start launches a sequential browser-signup batch.
func Start(req StartRequest) map[string]interface{} {
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 10 {
		req.Count = 10
	}
	if !req.ProxyConfigured {
		req.Proxy = ""
	}
	req.Engine = normalizeEngine(req.Engine)
	if req.EmailProvider == "" {
		req.EmailProvider = "outlook"
	}

	mgr.mu.Lock()
	if mgr.running {
		mgr.mu.Unlock()
		return map[string]interface{}{"error": "浏览器注册任务正在运行"}
	}
	if mgr.installing {
		mgr.mu.Unlock()
		return map[string]interface{}{"error": "正在安装浏览器引擎，请稍候"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	mgr.running = true
	mgr.currentEngine = req.Engine
	mgr.total = req.Count
	mgr.completed = 0
	mgr.success = 0
	mgr.failed = 0
	mgr.step = "starting"
	mgr.startTime = time.Now()
	mgr.cancel = cancel
	mgr.mu.Unlock()
	mgr.clearLogs()
	mgr.appendLog(fmt.Sprintf("[浏览器] 任务启动：引擎 %s，数量 %d", req.Engine, req.Count))

	go runBatch(ctx, req)
	return map[string]interface{}{"status": "started"}
}

// Stop cancels the running browser-signup batch.
func Stop() map[string]interface{} {
	mgr.mu.Lock()
	if !mgr.running || mgr.cancel == nil {
		mgr.mu.Unlock()
		return map[string]interface{}{"error": "没有正在运行的浏览器注册任务"}
	}
	cancel := mgr.cancel
	mgr.mu.Unlock()
	cancel()
	mgr.appendLog("[浏览器] 已请求停止")
	return map[string]interface{}{"status": "stopping"}
}

func runBatch(ctx context.Context, req StartRequest) {
	defer func() {
		mgr.mu.Lock()
		mgr.running = false
		mgr.step = "idle"
		mgr.cancel = nil
		mgr.mu.Unlock()
		mgr.appendLog("[浏览器] 任务结束")
	}()

	for i := 0; i < req.Count; i++ {
		if ctx.Err() != nil {
			mgr.appendLog("[浏览器] 任务已取消")
			return
		}
		mgr.mu.Lock()
		mgr.step = fmt.Sprintf("%d/%d", i+1, req.Count)
		mgr.mu.Unlock()

		result := runOne(ctx, req, i, req.Count, mgr.appendLog)
		success := result["status"] == "success"
		mgr.mu.Lock()
		mgr.completed++
		if success {
			mgr.success++
		} else {
			mgr.failed++
		}
		mgr.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
	}
}
