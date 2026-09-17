package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseServiceErrorRedactsCaptchaAndRepairsMessage(t *testing.T) {
	body := []byte(`{
		"requestId":"outer-id",
		"message":{
			"text":"è¯·å°è¯éæ°ç»å½",
			"heading":"åçæå¤éè¯¯",
			"type":"ERROR",
			"requestId":"message-id",
			"errorCode":"AUTHENTICATION_FAILED"
		},
		"captchaResponse":{"captchaToken":"secret-token","captchaCDN":"https://example.com"}
	}`)

	err := parseServiceError(body)
	if err == nil {
		t.Fatal("expected a parsed service error")
	}
	if err.Code != "AUTHENTICATION_FAILED" || err.RequestID != "message-id" || !err.Captcha {
		t.Fatalf("unexpected parsed error: %#v", err)
	}
	if err.Message != "请尝试重新登录" {
		t.Fatalf("message = %q", err.Message)
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "example.com") {
		t.Fatalf("sensitive CAPTCHA response leaked through error: %s", err)
	}
}

func TestParseServiceErrorFlatTESBlocked(t *testing.T) {
	err := parseServiceError([]byte(`{"errorCode":"BLOCKED","message":"Request was blocked by TES."}`))
	if err == nil {
		t.Fatal("expected a parsed TES error")
	}
	if err.Code != "BLOCKED" || err.Message != "Request was blocked by TES." {
		t.Fatalf("unexpected TES error: %#v", err)
	}
	if !isTESBlocked(err) {
		t.Fatal("TES BLOCKED was not classified as a TES block")
	}
}

func TestFormatLogBodyRepairsAWSMojibake(t *testing.T) {
	body := []byte(`{"requestId":"1e0b2715-0181-4f96-86ac-1f42a72d322c","message":{"text":"è¯·å°è¯éæ°ç»å½ãå¦æéè¯¯ä»ç¶å­å¨ï¼è¯·èç³»æ¨çç®¡çå","heading":"åçæå¤éè¯¯","errorCode":"ENTITY_DOES_NOT_EXIST"}}`)
	got := formatLogBody(body, 800)
	if !strings.Contains(got, "请尝试重新登录") {
		t.Fatalf("repaired body missing Chinese text: %q", got)
	}
	if !strings.Contains(got, "发生意外错误") {
		t.Fatalf("repaired heading missing Chinese text: %q", got)
	}
	if strings.Contains(got, "è¯·") {
		t.Fatalf("mojibake left in log body: %q", got)
	}
}

func TestFormatLogBodyLeavesTESEnglishAlone(t *testing.T) {
	body := []byte(`{"errorCode":"BLOCKED","message":"Request was blocked by TES."}`)
	got := formatLogBody(body, 800)
	if got != string(body) {
		t.Fatalf("formatLogBody() = %q", got)
	}
}

func TestFormatErrorTESBlocked(t *testing.T) {
	r := &Registrar{}
	got := r.formatError("SendOTP", fmt.Errorf("send-otp 失败 (400): %w", &ServiceError{
		Code:    "BLOCKED",
		Message: "Request was blocked by TES.",
	}))
	if !strings.Contains(got, "注册被拦截") || !strings.Contains(got, "BLOCKED") {
		t.Fatalf("formatError() = %q", got)
	}
	if strings.Contains(got, "è¯·") {
		t.Fatalf("formatError leaked mojibake: %q", got)
	}
}

func TestRepairMojibakeLeavesUTF8TextAlone(t *testing.T) {
	for _, value := range []string{"请稍后重试", "café", "plain text"} {
		if got := repairMojibake(value); got != value {
			t.Fatalf("repairMojibake(%q) = %q", value, got)
		}
	}
}

func TestUnexpectedServiceResponseNeverIncludesRawBody(t *testing.T) {
	err := unexpectedServiceResponse("密码设置未返回 redirect", []byte(`not-json captchaToken=secret-token`))
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "captchaToken") {
		t.Fatalf("raw response leaked through error: %s", err)
	}
}

func TestParseAWSWAFChallengeRequiresTokenAndScript(t *testing.T) {
	challenge, ok := parseAWSWAFChallenge([]byte(`{
		"captchaResponse": {
			"captchaToken": " redemption-token ",
			"captchaCDN": " https://example.com/jsapi.js "
		}
	}`))
	if !ok || challenge.RedemptionToken != "redemption-token" || challenge.JSAPIScript != "https://example.com/jsapi.js" {
		t.Fatalf("unexpected challenge: %#v, ok=%v", challenge, ok)
	}

	if _, ok := parseAWSWAFChallenge([]byte(`{"captchaResponse":{"captchaToken":"token"}}`)); ok {
		t.Fatal("incomplete challenge was accepted")
	}
}

func TestFormatAuthenticationFailure(t *testing.T) {
	r := &Registrar{}
	got := r.formatError("SetPassword", &ServiceError{
		Code:      "AUTHENTICATION_FAILED",
		Message:   "请尝试重新登录",
		RequestID: "request-id",
		Captcha:   true,
	})
	want := "设置密码失败: AWS 身份验证失败，验证令牌可能无效或已过期，响应包含 CAPTCHA 验证信息 (AUTHENTICATION_FAILED, requestId=request-id)"
	if got != want {
		t.Fatalf("formatError() = %q, want %q", got, want)
	}
}
