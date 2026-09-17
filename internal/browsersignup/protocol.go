package browsersignup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type workerMsg struct {
	Type   string `json:"type"`
	Level  string `json:"level,omitempty"`
	Msg    string `json:"msg,omitempty"`
	Error  string `json:"error,omitempty"`
	Email  string `json:"email,omitempty"`
	Step   string `json:"step,omitempty"`
	URL    string `json:"url,omitempty"`
	Result string `json:"result,omitempty"`
	Engine string `json:"engine,omitempty"`
	OK     bool   `json:"ok,omitempty"`
}

type workerProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	msgs   chan workerMsg
	done   chan error
	cancel context.CancelFunc
}

func startWorker(ctx context.Context, python string, jobPath string) (*workerProc, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, python, workerPath(), "--job", jobPath)
	cmd.SysProcAttr = hideWindowAttr()
	cmd.Env = append(os.Environ(),
		"PYTHONUNBUFFERED=1",
		"PYTHONIOENCODING=utf-8",
		"OMP_NUM_THREADS=1",
		"OPENBLAS_NUM_THREADS=1",
		"MKL_NUM_THREADS=1",
		"NUMEXPR_NUM_THREADS=1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	wp := &workerProc{
		cmd:    cmd,
		stdin:  stdin,
		msgs:   make(chan workerMsg, 64),
		done:   make(chan error, 1),
		cancel: cancel,
	}
	send := func(msg workerMsg) {
		select {
		case wp.msgs <- msg:
		case <-ctx.Done():
		}
	}
	go func() {
		// Camoufox/Playwright 进度条常用 \r 且不换行；按块读，避免管道堵死。
		readDelimited(stderr, func(raw []byte) {
			line := strings.TrimSpace(string(raw))
			if line != "" {
				send(workerMsg{Type: "log", Level: "debug", Msg: line})
			}
		})
	}()
	go func() {
		readDelimited(stdout, func(raw []byte) {
			line := strings.TrimSpace(string(raw))
			if line == "" {
				return
			}
			var msg workerMsg
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				send(workerMsg{Type: "log", Level: "debug", Msg: line})
				return
			}
			if msg.Type == "" {
				msg.Type = "log"
				msg.Msg = line
			}
			send(msg)
		})
		err := cmd.Wait()
		select {
		case wp.done <- err:
		case <-ctx.Done():
		}
	}()
	return wp, nil
}

func readDelimited(r io.Reader, onLine func([]byte)) {
	buf := make([]byte, 4096)
	var acc []byte
	flush := func(raw []byte) {
		if len(bytes.TrimSpace(raw)) == 0 {
			return
		}
		onLine(raw)
	}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			acc = append(acc, buf[:n]...)
			for {
				i := bytes.IndexAny(acc, "\r\n")
				if i < 0 {
					break
				}
				flush(acc[:i])
				acc = acc[i+1:]
			}
			if len(acc) > 4096 {
				flush(acc)
				acc = nil
			}
		}
		if err != nil {
			if len(acc) > 0 {
				flush(acc)
			}
			return
		}
	}
}

func (w *workerProc) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w.stdin, string(b)+"\n")
	return err
}

func (w *workerProc) sendOTP(code string) error {
	return w.send(map[string]string{"type": "otp", "code": code})
}

func (w *workerProc) stop() {
	_ = w.send(map[string]string{"type": "cancel"})
	w.cancel()
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
}

func writeJobFile(path string, job map[string]any) error {
	b, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("写入任务文件失败: %w", err)
	}
	return nil
}
