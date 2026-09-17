package browsersignup

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reg_go/internal/core"
	"reg_go/internal/data"
	"reg_go/internal/email"
	httputil "reg_go/internal/http"
	"reg_go/internal/storage"
)

var firstNames = []string{"James", "Alex", "Emma", "Noah", "Olivia", "Liam", "Sophia", "Mason", "Ava", "Ethan"}
var lastNames = []string{"Wilson", "Chen", "Miller", "Brooks", "Reed", "Parker", "Hayes", "Bennett", "Foster", "Cole"}

func randomFullName() string {
	return firstNames[rand.Intn(len(firstNames))] + " " + lastNames[rand.Intn(len(lastNames))]
}

func newTaskConfig(req StartRequest) *core.Config {
	settings := storage.GetAppSettings()
	httputil.SetRequestTimeoutSeconds(settings.RequestTimeoutSeconds)
	cfg := core.NewConfig()
	cfg.Debug = true
	cfg.OIDCBase = settings.OIDCBase
	cfg.SigninBase = settings.SigninBase
	cfg.ProfileBase = settings.ProfileBase
	cfg.ViewBase = settings.ViewBase
	cfg.PortalBase = settings.PortalBase
	cfg.StartURL = settings.StartURL
	cfg.KiroBase = settings.KiroBase
	cfg.KiroRedirectURI = settings.KiroRedirectURI
	cfg.DirectoryID = settings.DirectoryID
	cfg.OTPTimeout = settings.OTPTimeoutSeconds
	if cfg.OTPTimeout < 180 {
		cfg.OTPTimeout = 180
	}
	cfg.TelemetryEnabled = settings.TelemetryEnabled
	cfg.HTTPRetries = map[string]int{"fast": 0, "standard": 2, "stable": 3}[settings.RetryProfile]
	cfg.Password = core.GenPassword()
	cfg.FullName = randomFullName()
	if req.ProxyConfigured {
		cfg.Proxy = strings.TrimSpace(req.Proxy)
	}
	switch settings.EmailProxyMode {
	case "follow-task":
		cfg.EmailProxy = cfg.Proxy
	case "custom":
		cfg.EmailProxy = settings.EmailProxy
	}
	applyEmailProvider(cfg, req.EmailProvider)
	return cfg
}

func runOne(ctx context.Context, req StartRequest, index, total int, logFn func(string)) map[string]interface{} {
	prefix := fmt.Sprintf("[浏览器][%d/%d]", index+1, total)
	engine := normalizeEngine(req.Engine)
	python, _, err := resolvePython()
	if err != nil {
		logFn(prefix + " " + err.Error())
		return map[string]interface{}{"status": "failed", "error": err.Error()}
	}
	ok, msg := engineInstalled(python, engine)
	if !ok {
		errMsg := "浏览器引擎未就绪: " + engine
		if msg != "" {
			errMsg += " (" + msg + ")"
		}
		logFn(prefix + " " + errMsg)
		return map[string]interface{}{"status": "failed", "error": errMsg}
	}
	if err := extractWorker(); err != nil {
		logFn(prefix + " 写出 worker 失败: " + err.Error())
		return map[string]interface{}{"status": "failed", "error": err.Error()}
	}

	cfg := newTaskConfig(req)
	emailAddr, err := attachEmail(ctx, cfg, req, index)
	if err != nil {
		logFn(prefix + " " + err.Error())
		return map[string]interface{}{"status": "failed", "error": err.Error()}
	}
	logFn(fmt.Sprintf("%s 使用邮箱 %s，引擎 %s", prefix, emailAddr, engine))

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	reg := core.NewRegistrar(cfg)
	reg.Ctx = runCtx
	reg.TaskLabel = fmt.Sprintf("%d/%d", index+1, total)

	if err := reg.Step1OIDC(); err != nil {
		friendly := reg.FriendlyError("OIDC", err)
		logFn(prefix + " " + friendly)
		return map[string]interface{}{"status": "failed", "error": friendly, "email": emailAddr}
	}
	if err := reg.Step2Device(); err != nil {
		friendly := reg.FriendlyError("Device", err)
		logFn(prefix + " " + friendly)
		return map[string]interface{}{"status": "failed", "error": friendly, "email": emailAddr}
	}
	if err := reg.Step3Email(); err != nil {
		friendly := reg.FriendlyError("Email", err)
		logFn(prefix + " " + friendly)
		return map[string]interface{}{"status": "failed", "error": friendly, "email": emailAddr}
	}

	deviceURL := fmt.Sprintf("%s/start/#/device?user_code=%s", cfg.ViewBase, reg.UserCode)
	logFn(fmt.Sprintf("%s 设备码已生成，启动指纹浏览器", prefix))

	jobDir := filepath.Join(engineDir(), "jobs")
	_ = os.MkdirAll(jobDir, 0o755)
	jobPath := filepath.Join(jobDir, fmt.Sprintf("job-%d-%d.json", time.Now().UnixNano(), index))
	job := map[string]any{
		"engine":              engine,
		"headless":            req.Headless,
		"proxy":               cfg.Proxy,
		"device_url":          deviceURL,
		"user_code":           reg.UserCode,
		"email":               reg.Email,
		"password":            cfg.Password,
		"full_name":           cfg.FullName,
		"otp_timeout_sec":     cfg.OTPTimeout,
		"overall_timeout_sec": 600,
	}
	if err := writeJobFile(jobPath, job); err != nil {
		return map[string]interface{}{"status": "failed", "error": err.Error(), "email": emailAddr}
	}
	defer os.Remove(jobPath)

	worker, err := startWorker(runCtx, python, jobPath)
	if err != nil {
		logFn(prefix + " 启动浏览器进程失败: " + err.Error())
		return map[string]interface{}{"status": "failed", "error": err.Error(), "email": emailAddr}
	}
	defer worker.stop()

	tokenCh := make(chan map[string]interface{}, 1)
	errCh := make(chan error, 1)
	go func() {
		tok, err := reg.PollDeviceToken(8 * time.Minute)
		if err != nil {
			errCh <- err
			return
		}
		tokenCh <- tok
	}()

	var workerErr string
	authorized := false
	for {
		select {
		case <-ctx.Done():
			return map[string]interface{}{"status": "failed", "error": "任务已取消", "email": emailAddr}
		case tok := <-tokenCh:
			return finishSuccess(reg, cfg, emailAddr, tok, prefix, logFn)
		case err := <-errCh:
			if authorized {
				friendly := reg.FriendlyError("DeviceToken", err)
				logFn(prefix + " " + friendly)
				return map[string]interface{}{"status": "failed", "error": friendly, "email": emailAddr, "passwordSet": true}
			}
			if workerErr != "" {
				return map[string]interface{}{"status": "failed", "error": workerErr, "email": emailAddr}
			}
			friendly := reg.FriendlyError("DeviceToken", err)
			logFn(prefix + " " + friendly)
			return map[string]interface{}{"status": "failed", "error": friendly, "email": emailAddr}
		case msg, ok := <-worker.msgs:
			if !ok {
				continue
			}
			switch msg.Type {
			case "log":
				if msg.Msg != "" {
					logFn(prefix + " " + msg.Msg)
				}
			case "status":
				if msg.Step != "" {
					logFn(fmt.Sprintf("%s 步骤 %s", prefix, msg.Step))
				}
			case "need_otp":
				logFn(prefix + " 浏览器请求验证码，开始收信")
				otp, err := reg.Step10GetOTP()
				if err != nil {
					friendly := reg.FriendlyError("GetOTP", err)
					logFn(prefix + " " + friendly)
					workerErr = friendly
					worker.stop()
					continue
				}
				if err := worker.sendOTP(otp); err != nil {
					logFn(prefix + " 回传验证码失败: " + err.Error())
					workerErr = err.Error()
					worker.stop()
				}
			case "device_authorized":
				authorized = true
				logFn(prefix + " 浏览器已完成设备授权，等待令牌")
			case "error":
				workerErr = msg.Error
				if workerErr == "" {
					workerErr = "浏览器注册失败"
				}
				logFn(prefix + " " + workerErr)
			case "done":
				if msg.Result == "authorized" {
					authorized = true
				}
			}
		case err := <-worker.done:
			if ctx.Err() != nil {
				return map[string]interface{}{"status": "failed", "error": "任务已取消", "email": emailAddr}
			}
			if workerErr != "" && !authorized {
				return map[string]interface{}{"status": "failed", "error": workerErr, "email": emailAddr}
			}
			if err != nil && !authorized {
				msg := "浏览器进程退出"
				if err.Error() != "" {
					msg += ": " + err.Error()
				}
				logFn(prefix + " " + msg)
				return map[string]interface{}{"status": "failed", "error": msg, "email": emailAddr}
			}
			select {
			case tok := <-tokenCh:
				return finishSuccess(reg, cfg, emailAddr, tok, prefix, logFn)
			case pollErr := <-errCh:
				friendly := reg.FriendlyError("DeviceToken", pollErr)
				logFn(prefix + " " + friendly)
				return map[string]interface{}{"status": "failed", "error": friendly, "email": emailAddr, "passwordSet": authorized}
			case <-ctx.Done():
				return map[string]interface{}{"status": "failed", "error": "任务已取消", "email": emailAddr}
			case <-time.After(90 * time.Second):
				return map[string]interface{}{"status": "failed", "error": "设备授权令牌等待超时", "email": emailAddr, "passwordSet": authorized}
			}
		}
	}
}

func finishSuccess(reg *core.Registrar, cfg *core.Config, emailAddr string, awsToken map[string]interface{}, prefix string, logFn func(string)) map[string]interface{} {
	verify := reg.VerifyAlive(awsToken)
	if suspended, _ := verify["suspended"].(bool); suspended {
		logFn(prefix + " 账号已被封禁")
		return map[string]interface{}{"status": "failed", "error": "suspended", "email": emailAddr, "passwordSet": true}
	}
	if alive, _ := verify["alive"].(bool); alive {
		logFn(prefix + " 注册成功")
	} else {
		logFn(prefix + " 注册完成")
	}
	result := map[string]interface{}{
		"email":         emailAddr,
		"password":      cfg.Password,
		"status":        "success",
		"passwordSet":   true,
		"client_id":     reg.ClientID,
		"client_secret": reg.ClientSecret,
		"device_code":   reg.DeviceCode,
		"aws_token":     awsToken,
		"verify":        verify,
		"mode":          "browser",
	}
	outDir := storage.GetResultOutputDir()
	if err := data.SaveKiroSuccess(result, outDir); err != nil {
		logFn(prefix + " 保存账号失败: " + err.Error())
	} else {
		logFn(prefix + " 已写入 accounts.json")
	}
	if cfg.UseOutlook {
		email.UpdateAccountStatus(emailAddr, true, true)
	}
	return result
}
