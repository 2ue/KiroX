package browsersignup

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reg_go/internal/storage"
)

func engineDir() string {
	return filepath.Join(storage.GetDataDir(), "browser_engine")
}

func venvPythonPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(engineDir(), "venv", "Scripts", "python.exe")
	}
	return filepath.Join(engineDir(), "venv", "bin", "python")
}

func workerPath() string {
	return filepath.Join(engineDir(), "worker.py")
}

func extractWorker() error {
	if err := os.MkdirAll(engineDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(workerPath(), workerPy, 0o644)
}

func pythonExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func runPython(ctx context.Context, python string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, python, args...)
	cmd.SysProcAttr = hideWindowAttr()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if out != "" {
			msg = out + "\n" + msg
		}
		return out, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func findSystemPython() (string, string, error) {
	if env := strings.TrimSpace(os.Getenv("BROWSER_SIGNUP_PYTHON")); env != "" && pythonExists(env) {
		ver, err := pythonVersion(env)
		if err == nil {
			return env, ver, nil
		}
	}
	candidates := [][]string{
		{"py", "-3"},
		{"python"},
		{"python3"},
	}
	for _, c := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		cmd := exec.CommandContext(ctx, c[0], append(c[1:], "-c", "import sys; print(sys.executable); print(sys.version.split()[0])")...)
		cmd.SysProcAttr = hideWindowAttr()
		out, err := cmd.Output()
		cancel()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) == 0 {
			continue
		}
		exe := strings.TrimSpace(lines[0])
		ver := ""
		if len(lines) > 1 {
			ver = strings.TrimSpace(lines[1])
		}
		if pythonExists(exe) {
			return exe, ver, nil
		}
	}
	return "", "", fmt.Errorf("未找到 Python 3，请先安装 Python 3.10+")
}

func pythonVersion(python string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := runPython(ctx, python, "-c", "import sys; print(sys.version.split()[0])")
	if err != nil {
		return "", err
	}
	return out, nil
}

func resolvePython() (string, string, error) {
	venv := venvPythonPath()
	if pythonExists(venv) {
		ver, err := pythonVersion(venv)
		if err == nil {
			return venv, ver, nil
		}
	}
	return findSystemPython()
}

func engineInstalled(python, engine string) (bool, string) {
	if python == "" {
		return false, "未找到 Python"
	}
	if err := extractWorker(); err != nil {
		return false, err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := runPython(ctx, python, workerPath(), "--check", engine)
	if err != nil {
		return false, strings.TrimSpace(err.Error())
	}
	if strings.Contains(out, `"ok": true`) || strings.Contains(out, `"ok":true`) {
		return true, ""
	}
	return false, out
}

// EngineStatus describes local Python / Camoufox / Playwright availability.
type EngineStatus struct {
	PythonPath       string `json:"pythonPath"`
	PythonVersion    string `json:"pythonVersion"`
	PythonFound      bool   `json:"pythonFound"`
	VenvReady        bool   `json:"venvReady"`
	CamoufoxReady    bool   `json:"camoufoxReady"`
	PlaywrightReady  bool   `json:"playwrightReady"`
	CamoufoxError    string `json:"camoufoxError,omitempty"`
	PlaywrightError  string `json:"playwrightError,omitempty"`
	Installing       bool   `json:"installing"`
	InstallEngine    string `json:"installEngine,omitempty"`
	WorkerExtracted  bool   `json:"workerExtracted"`
}

func snapshotEngineStatus() EngineStatus {
	st := EngineStatus{
		VenvReady: pythonExists(venvPythonPath()),
	}
	mgr.mu.Lock()
	st.Installing = mgr.installing
	st.InstallEngine = mgr.installEngine
	mgr.mu.Unlock()

	python, ver, err := resolvePython()
	if err != nil {
		st.CamoufoxError = err.Error()
		st.PlaywrightError = err.Error()
		return st
	}
	st.PythonPath = python
	st.PythonVersion = ver
	st.PythonFound = true
	if err := extractWorker(); err == nil {
		st.WorkerExtracted = true
	}
	var wg sync.WaitGroup
	var camOK, pwOK bool
	var camErr, pwErr string
	wg.Add(2)
	go func() {
		defer wg.Done()
		camOK, camErr = engineInstalled(python, "camoufox")
	}()
	go func() {
		defer wg.Done()
		pwOK, pwErr = engineInstalled(python, "playwright")
	}()
	wg.Wait()
	st.CamoufoxReady = camOK
	st.CamoufoxError = camErr
	st.PlaywrightReady = pwOK
	st.PlaywrightError = pwErr
	return st
}

func ensureVenv(ctx context.Context, logFn func(string)) (string, error) {
	venvPy := venvPythonPath()
	if pythonExists(venvPy) {
		return venvPy, nil
	}
	sysPy, ver, err := findSystemPython()
	if err != nil {
		return "", err
	}
	logFn(fmt.Sprintf("使用系统 Python %s 创建虚拟环境", ver))
	if err := os.MkdirAll(engineDir(), 0o755); err != nil {
		return "", err
	}
	if _, err := runPython(ctx, sysPy, "-m", "venv", filepath.Join(engineDir(), "venv")); err != nil {
		return "", fmt.Errorf("创建 venv 失败: %w", err)
	}
	if !pythonExists(venvPy) {
		return "", fmt.Errorf("venv 创建后仍找不到 %s", venvPy)
	}
	return venvPy, nil
}

func streamCommand(ctx context.Context, logFn func(string), python string, args ...string) error {
	cmd := exec.CommandContext(ctx, python, args...)
	cmd.SysProcAttr = hideWindowAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scan := func(s *bufio.Scanner) {
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line != "" {
				logFn(line)
			}
		}
	}
	go scan(bufio.NewScanner(stdout))
	go scan(bufio.NewScanner(stderr))
	return cmd.Wait()
}

func installEngine(ctx context.Context, engine string, logFn func(string)) error {
	engine = normalizeEngine(engine)
	python, err := ensureVenv(ctx, logFn)
	if err != nil {
		return err
	}
	if err := extractWorker(); err != nil {
		return err
	}
	logFn("升级 pip")
	_ = streamCommand(ctx, logFn, python, "-m", "pip", "install", "-U", "pip")
	switch engine {
	case "playwright":
		logFn("安装 Playwright")
		if err := streamCommand(ctx, logFn, python, "-m", "pip", "install", "playwright"); err != nil {
			return fmt.Errorf("安装 playwright 失败: %w", err)
		}
		logFn("下载 Chromium")
		if err := streamCommand(ctx, logFn, python, "-m", "playwright", "install", "chromium"); err != nil {
			return fmt.Errorf("playwright install chromium 失败: %w", err)
		}
	default:
		logFn("安装 Camoufox")
		if err := streamCommand(ctx, logFn, python, "-m", "pip", "install", "camoufox[geoip]"); err != nil {
			return fmt.Errorf("安装 camoufox 失败: %w", err)
		}
		logFn("下载 Camoufox 浏览器内核")
		if err := streamCommand(ctx, logFn, python, "-m", "camoufox", "fetch"); err != nil {
			return fmt.Errorf("camoufox fetch 失败: %w", err)
		}
	}
	ok, msg := engineInstalled(python, engine)
	if !ok {
		if msg == "" {
			msg = "安装后检测仍未通过"
		}
		return fmt.Errorf("%s", msg)
	}
	logFn("引擎已就绪: " + engine)
	return nil
}

func normalizeEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "playwright", "chromium", "chrome":
		return "playwright"
	default:
		return "camoufox"
	}
}
