package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"reg_go/internal/browsersignup"
	"reg_go/internal/email"
	"reg_go/internal/proxy"
	"reg_go/internal/storage"
)

var credRe = regexp.MustCompile(`(?i)(//|@)[^/\s:]+:[^/\s@]+@`)

func redact(s string) string {
	s = credRe.ReplaceAllString(s, "${1}<redacted>@")
	return s
}

func main() {
	proxy.InitPool(storage.GetDataDir())
	var proxyURL, proxyName, proxyCity, proxyType string
	var residential []proxy.PoolEntry
	for _, e := range proxy.List() {
		if e.Enabled && e.ProbeType == "residential" && e.ProbeOK {
			residential = append(residential, e)
		}
	}
	if n := len(residential); n > 0 {
		e := residential[int(time.Now().UnixNano()%int64(n))]
		proxyURL = e.URL
		proxyName = e.Name
		proxyCity = e.ProbeCity
		proxyType = e.ProbeType
	}
	if proxyURL == "" {
		fmt.Fprintln(os.Stderr, "没有可用的家宽代理")
		os.Exit(1)
	}
	fmt.Printf("proxy %s city=%s type=%s\n", proxyName, proxyCity, proxyType)

	res := browsersignup.Start(browsersignup.StartRequest{
		Engine:          "camoufox",
		Headless:        false,
		Count:           1,
		Proxy:           proxyURL,
		ProxyConfigured: true,
		EmailProvider:   "mailalias",
		MailAliasConfig: email.MailAliasConfig{
			BaseURL: email.DefaultMailAliasBaseURL,
			Prefix:  "minhchau51863",
			Mode:    "mixed",
			Length:  8,
		},
	})
	if msg, ok := res["error"].(string); ok && strings.TrimSpace(msg) != "" {
		fmt.Fprintln(os.Stderr, "start error:", msg)
		os.Exit(1)
	}

	last := 0
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		logs := browsersignup.GetLogs()
		for i := last; i < len(logs); i++ {
			fmt.Println(redact(logs[i]))
		}
		last = len(logs)
		st := browsersignup.GetStatus()
		running, _ := st["running"].(bool)
		if !running {
			fmt.Printf("done success=%v failed=%v step=%v\n", st["success"], st["failed"], st["step"])
			if v, _ := st["success"].(int); v > 0 {
				return
			}
			os.Exit(2)
		}
		time.Sleep(time.Second)
	}
	fmt.Fprintln(os.Stderr, "runner wait timeout")
	os.Exit(3)
}
