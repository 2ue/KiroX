#!/usr/bin/env python3
"""Fingerprint-browser worker for AWS Builder ID device signup.

Talks to the Go orchestrator over NDJSON on stdin/stdout.
"""
from __future__ import annotations

import argparse
import json
import os
import queue
import random
import re
import sys
import threading
import time
from typing import Any, Dict, Optional
from urllib.parse import urlparse

SUCCESS_HINTS = (
    "you can close this",
    "you may close this",
    "device verified",
    "request approved",
    "successfully authorized",
    "authorization successful",
    "device confirmed",
    "已授权",
    "可以关闭",
    "允许访问",
)
BLOCKED_HINTS = (
    "request was blocked",
    "blocked by tes",
    '"errorcode":"blocked"',
    "errorcode\":\"blocked",
    "err-837",
)
SIGNUP_ERROR_HINTS = (
    "err-837",
    "error processing your request",
    "sorry, there was an error",
    "there was an error processing",
)
EXISTING_HINTS = (
    "forgot password",
    "forgot your password",
    "reset password",
    "remember me",
)
OTP_HINTS = (
    "verification code",
    "one-time",
    "one time",
    "security code",
    "enter the code",
    "we sent a code",
    "sent a verification",
    "验证码",
    "一次性密码",
)
EMAIL_HINTS = (
    "email",
    "e-mail",
    "builder id",
    "sign in",
    "log in",
    "邮箱",
    "电子邮件",
)
CREATE_HINTS = (
    "create a builder id",
    "create aws builder",
    "create your aws builder",
    "don't have an aws",
    "do not have an aws",
    "create account",
    "sign up",
    "创建",
    "注册",
)
PASSWORD_HINTS = (
    "create password",
    "choose a password",
    "set password",
    "new password",
    "confirm password",
    "re-enter password",
    "设置密码",
    "创建密码",
    "确认密码",
)
NAME_HINTS = (
    "full name",
    "given name",
    "first name",
    "family name",
    "last name",
    "your name",
    "姓名",
    "名字",
    "姓氏",
)
ALLOW_HINTS = (
    "allow",
    "confirm",
    "authorize",
    "approve",
    "continue to kiro",
    "允许",
    "确认",
    "授权",
)
NEXT_HINTS = (
    "next",
    "continue",
    "submit",
    "send code",
    "send verification",
    "verify",
    "下一步",
    "继续",
    "提交",
    "发送验证码",
    "发送",
)
COOKIE_HINTS = (
    "accept all",
    "accept cookies",
    "got it",
    "i understand",
    "同意",
    "接受",
)


_protocol_out = sys.stdout


def _redirect_stdio_for_browser() -> None:
    """Keep NDJSON on the original stdout fd; send everything else to stderr.

    Playwright/Camoufox write ``\\r`` progress to C-level fd 1. Redirecting
    only ``sys.stdout`` does not change fd 1, so a line-based Go reader can
    fill the Windows pipe and deadlock the import.
    """
    global _protocol_out
    try:
        sys.stdout.flush()
        sys.stderr.flush()
        proto_fd = os.dup(sys.stdout.fileno())
        _protocol_out = os.fdopen(proto_fd, "w", encoding="utf-8", buffering=1, closefd=True)
        os.dup2(sys.stderr.fileno(), sys.stdout.fileno())
        sys.stdout = sys.stderr
    except Exception:
        sys.stdout = sys.stderr


def emit(obj: Dict[str, Any]) -> None:
    _protocol_out.write(json.dumps(obj, ensure_ascii=False) + "\n")
    _protocol_out.flush()


def log(msg: str, level: str = "info") -> None:
    emit({"type": "log", "level": level, "msg": msg})


def fail(msg: str) -> None:
    emit({"type": "error", "error": msg})
    emit({"type": "done", "result": "error"})


class Bridge:
    def __init__(self) -> None:
        self.otp: "queue.Queue[str]" = queue.Queue()
        self.cancel = threading.Event()
        t = threading.Thread(target=self._read, daemon=True)
        t.start()

    def _read(self) -> None:
        for raw in sys.stdin:
            line = raw.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                continue
            kind = str(msg.get("type") or "")
            if kind == "otp":
                self.otp.put(str(msg.get("code") or "").strip())
            elif kind in ("cancel", "stop"):
                self.cancel.set()

    def cancelled(self) -> bool:
        return self.cancel.is_set()

    def wait_otp(self, timeout: float) -> str:
        try:
            return self.otp.get(timeout=timeout)
        except queue.Empty:
            return ""


def parse_proxy(raw: Optional[str]) -> Optional[Dict[str, str]]:
    if not raw:
        return None
    u = urlparse(raw)
    if not u.hostname:
        return None
    scheme = u.scheme or "http"
    port = u.port or (443 if scheme == "https" else 80)
    server = "%s://%s:%s" % (scheme, u.hostname, port)
    out = {"server": server}
    if u.username:
        out["username"] = u.username
    if u.password:
        out["password"] = u.password
    return out


def iter_targets(page):
    out = [page]
    try:
        for fr in page.frames:
            if fr not in out:
                out.append(fr)
    except Exception:
        pass
    return out


def body_text(page) -> str:
    chunks = []
    for t in iter_targets(page):
        try:
            chunks.append((t.inner_text("body") or "")[:4000])
        except Exception:
            continue
    return "\n".join(chunks)[:8000]


def text_snippet(text: str) -> str:
    return re.sub(r"\s+", " ", text or "").strip()[:180]


def snapshot(page, tag: str) -> None:
    try:
        jobs = os.path.join(os.path.dirname(os.path.abspath(__file__)), "jobs")
        os.makedirs(jobs, exist_ok=True)
        path = os.path.join(jobs, "last-%s.png" % tag)
        page.screenshot(path=path, full_page=True)
        log("截图已保存")
    except Exception:
        pass


def contains_any(text: str, hints: tuple) -> bool:
    low = text.lower()
    return any(h in low for h in hints)


def fill_react(locator, text: str) -> None:
    locator.evaluate(
        """(el, v) => {
            el.focus();
            const proto = el.tagName === 'TEXTAREA'
                ? window.HTMLTextAreaElement.prototype
                : window.HTMLInputElement.prototype;
            const desc = Object.getOwnPropertyDescriptor(proto, 'value');
            const setVal = (val) => {
                if (desc && desc.set) desc.set.call(el, val);
                else el.value = val;
            };
            setVal('');
            el.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'deleteContentBackward' }));
            setVal(v);
            el.dispatchEvent(new InputEvent('input', {
                bubbles: true,
                cancelable: true,
                inputType: 'insertText',
                data: v,
            }));
            el.dispatchEvent(new Event('change', { bubbles: true }));
        }""",
        text,
    )


def type_human(locator, text: str) -> None:
    try:
        fill_react(locator, text)
        current = (locator.input_value() or "").strip()
        if current == text:
            return
    except Exception:
        pass
    try:
        locator.fill(text)
        current = (locator.input_value() or "").strip()
        if current == text:
            return
    except Exception:
        pass
    try:
        locator.press_sequentially(text, delay=random.randint(35, 80))
        return
    except Exception:
        pass
    try:
        locator.type(text, delay=random.randint(35, 80))
    except Exception:
        fill_react(locator, text)


def first_visible(root, selectors, timeout_ms: int = 1500):
    for sel in selectors:
        try:
            loc = root.locator(sel).first
            if loc.count() == 0:
                continue
            if loc.is_visible(timeout=timeout_ms):
                return loc
        except Exception:
            continue
    return None


def first_visible_any(page, selectors, timeout_ms: int = 1500):
    for t in iter_targets(page):
        loc = first_visible(t, selectors, timeout_ms)
        if loc is not None:
            return loc
    return None


def real_click(page, locator) -> bool:
    try:
        box = locator.bounding_box()
        if not box or box.get("width", 0) < 2 or box.get("height", 0) < 2:
            locator.click(timeout=4000)
            return True
        x = box["x"] + box["width"] * random.uniform(0.35, 0.65)
        y = box["y"] + box["height"] * random.uniform(0.35, 0.65)
        page.mouse.move(x, y, steps=random.randint(10, 22))
        time.sleep(random.uniform(0.08, 0.22))
        page.mouse.down()
        time.sleep(random.uniform(0.05, 0.14))
        page.mouse.up()
        return True
    except Exception:
        try:
            locator.click(timeout=4000)
            return True
        except Exception:
            return False


def click_named(page, names: tuple, timeout_ms: int = 1200) -> bool:
    for t in iter_targets(page):
        for name in names:
            for kind in ("button", "link"):
                try:
                    el = t.get_by_role(kind, name=re.compile(name, re.I)).first
                    if el.count() and el.is_visible(timeout=timeout_ms):
                        if real_click(page, el):
                            return True
                except Exception:
                    continue
            try:
                el = t.get_by_text(re.compile(r"^%s$" % re.escape(name), re.I)).first
                if el.count() and el.is_visible(timeout=timeout_ms):
                    if real_click(page, el):
                        return True
            except Exception:
                continue
    return False


def dismiss_cookies(page) -> None:
    click_named(page, COOKIE_HINTS, 600)


def fill_user_code(page, code: str) -> bool:
    if not code:
        return False
    loc = first_visible_any(page, [
        'input[name="user_code"]',
        'input[name="userCode"]',
        'input[id*="user-code"]',
        'input[id*="userCode"]',
        'input[id*="deviceCode"]',
        'input[placeholder*="XXXX"]',
    ], 800)
    if loc is None:
        return False
    try:
        if (loc.get_attribute("maxlength") or "") == "1":
            return False
        if (loc.get_attribute("type") or "").lower() == "email":
            return False
    except Exception:
        pass
    type_human(loc, code)
    return True


def fill_email(page, email: str) -> bool:
    loc = first_visible_any(page, [
        'input[type="email"]',
        'input[name="email"]',
        'input[autocomplete="username"]',
        'input[autocomplete="email"]',
        '#email',
        'input[inputmode="email"]',
    ])
    if loc is None:
        for t in iter_targets(page):
            try:
                cand = t.get_by_label(re.compile(r"email|e-mail|邮箱", re.I)).first
                if cand.count():
                    loc = cand
                    break
            except Exception:
                continue
    if loc is None:
        return False
    current = ""
    try:
        current = loc.input_value() or ""
    except Exception:
        pass
    if current.strip().lower() != email.lower():
        type_human(loc, email)
    return True


def fill_otp(page, code: str) -> bool:
    for t in iter_targets(page):
        boxes = t.locator('input[maxlength="1"]')
        try:
            n = boxes.count()
        except Exception:
            n = 0
        if n >= 4:
            for i, ch in enumerate(code[:n]):
                try:
                    boxes.nth(i).fill(ch)
                except Exception:
                    type_human(boxes.nth(i), ch)
            return True
    loc = first_visible_any(page, [
        'input[autocomplete="one-time-code"]',
        'input[name="otp"]',
        'input[name="code"]',
        'input[inputmode="numeric"]',
        'input[type="tel"]',
    ])
    if loc is None:
        for t in iter_targets(page):
            try:
                cand = t.get_by_label(re.compile(r"code|otp|验证码", re.I)).first
                if cand.count():
                    loc = cand
                    break
            except Exception:
                continue
    if loc is None:
        return False
    type_human(loc, code)
    return True


def fill_passwords(page, password: str) -> bool:
    filled = 0
    for t in iter_targets(page):
        locs = t.locator('input[type="password"]')
        try:
            n = locs.count()
        except Exception:
            n = 0
        if n == 0:
            continue
        for i in range(min(n, 2)):
            try:
                el = locs.nth(i)
                if el.is_visible(timeout=800):
                    type_human(el, password)
                    filled += 1
            except Exception:
                continue
        if filled > 0:
            return True
    return filled > 0


def labeled_input(page, pattern: str):
    for t in iter_targets(page):
        try:
            cand = t.get_by_label(re.compile(pattern, re.I)).first
            if cand.count() and cand.is_visible(timeout=800):
                return cand
        except Exception:
            continue
    return None


def dump_inputs(page) -> None:
    bits = []
    for t in iter_targets(page):
        try:
            loc = t.locator("input, textarea")
            n = min(int(loc.count()), 12)
        except Exception:
            continue
        for i in range(n):
            el = loc.nth(i)
            try:
                bits.append("type=%s name=%s id=%s aria=%s ph=%s vis=%s" % (
                    el.get_attribute("type") or "",
                    el.get_attribute("name") or "",
                    el.get_attribute("id") or "",
                    el.get_attribute("aria-label") or "",
                    el.get_attribute("placeholder") or "",
                    el.is_visible(timeout=200),
                ))
            except Exception:
                continue
    if bits:
        log("表单控件: " + " | ".join(bits))


def visible_text_inputs(page):
    out = []
    for t in iter_targets(page):
        try:
            loc = t.locator("input, textarea")
            n = min(int(loc.count()), 20)
        except Exception:
            continue
        for i in range(n):
            el = loc.nth(i)
            try:
                typ = (el.get_attribute("type") or "text").lower()
                if typ in ("hidden", "checkbox", "radio", "submit", "button", "file", "email", "password"):
                    continue
                if not el.is_visible(timeout=400):
                    continue
                out.append(el)
            except Exception:
                continue
    return out


def fill_name(page, full_name: str) -> bool:
    parts = full_name.split(None, 1)
    first = parts[0] if parts else "Alex"
    last = parts[1] if len(parts) > 1 else "Chen"
    try:
        page.wait_for_selector("input, textarea", timeout=8000)
    except Exception:
        pass
    try:
        return _fill_name_inner(page, full_name, first, last)
    except Exception as e:
        log("填写姓名失败: %s" % e)
        return False


def _fill_name_inner(page, full_name: str, first: str, last: str) -> bool:
    first_loc = first_visible_any(page, [
        'input[name="givenName"]',
        'input[name="firstName"]',
        'input[autocomplete="given-name"]',
        'input[id*="given"]',
        'input[id*="first"]',
        'input[aria-label*="First" i]',
    ]) or labeled_input(page, r"first name|given name|名")
    last_loc = first_visible_any(page, [
        'input[name="familyName"]',
        'input[name="lastName"]',
        'input[autocomplete="family-name"]',
        'input[id*="family"]',
        'input[id*="last"]',
        'input[aria-label*="Last" i]',
    ]) or labeled_input(page, r"last name|family name|surname|姓")
    full_loc = first_visible_any(page, [
        'input[name="fullName"]',
        'input[name="name"]',
        'input[autocomplete="name"]',
        'input[id*="fullName"]',
        'input[id*="full-name"]',
        'input[aria-label="Name" i]',
        'input[aria-label*="your name" i]',
        'input[placeholder*="Silva" i]',
        'input[placeholder*="Maria" i]',
        'input[id^="formField"]',
    ]) or labeled_input(page, r"^(name|full name|your name|姓名)$")
    did = False
    if first_loc is not None:
        type_human(first_loc, first)
        did = True
    if last_loc is not None:
        type_human(last_loc, last)
        did = True
    if not did and full_loc is not None:
        type_human(full_loc, full_name)
        did = True
    if not did:
        texts = visible_text_inputs(page)
        if len(texts) >= 2:
            type_human(texts[0], first)
            type_human(texts[1], last)
            did = True
        elif len(texts) == 1:
            type_human(texts[0], full_name)
            did = True
    if not did:
        for t in iter_targets(page):
            try:
                lab = t.get_by_text(re.compile(r"^(name|your name|full name|姓名)$", re.I)).first
                if not lab.count() or not lab.is_visible(timeout=600):
                    continue
                lab.click(timeout=3000)
                time.sleep(0.2)
                page.keyboard.type(full_name, delay=random.randint(40, 90))
                did = True
                break
            except Exception:
                continue
    return did


def looks_success(text: str, url: str) -> bool:
    if contains_any(text, SUCCESS_HINTS):
        return True
    low = url.lower()
    return "authorized" in low or "success" in low and "device" in low


def looks_blocked(text: str) -> bool:
    return contains_any(text, BLOCKED_HINTS) or "blocked by tes" in text.lower()


def looks_signup_error(text: str) -> bool:
    return contains_any(text, SIGNUP_ERROR_HINTS)


def attach_network(page) -> None:
    def on_response(resp) -> None:
        url = resp.url or ""
        low = url.lower()
        if any(skip in low for skip in (".js", ".css", ".png", ".svg", ".woff", "static/", "cloudfront")):
            return
        if not any(hit in low for hit in (
            "send-otp", "create-identity", "createidentity", "signup",
            "profile.aws.amazon", "/api/", "otp",
        )):
            return
        try:
            status = resp.status
        except Exception:
            return
        snippet = ""
        try:
            if status >= 400 or any(hit in low for hit in ("otp", "identity", "signup")):
                snippet = re.sub(r"\s+", " ", (resp.text() or "")[:280])
        except Exception:
            pass
        log("网络 %s %s %s" % (status, url[:160], snippet[:180]))

    page.on("response", on_response)


def preload_engine(engine: str) -> None:
    os.environ.setdefault("OMP_NUM_THREADS", "1")
    os.environ.setdefault("OPENBLAS_NUM_THREADS", "1")
    os.environ.setdefault("MKL_NUM_THREADS", "1")
    os.environ.setdefault("NUMEXPR_NUM_THREADS", "1")
    engine = (engine or "camoufox").lower()
    if engine == "playwright":
        log("准备导入 Playwright")
        from playwright.sync_api import sync_playwright  # noqa: F401
        log("Playwright 已导入")
        return
    log("准备导入 Camoufox")
    from camoufox.sync_api import Camoufox  # noqa: F401
    log("Camoufox 已导入")


def launch_camoufox(job: Dict[str, Any]):
    from camoufox.sync_api import Camoufox

    proxy = parse_proxy(job.get("proxy"))
    kwargs = {
        "headless": bool(job.get("headless")),
        "humanize": False,
        "geoip": False,
    }
    if sys.platform == "win32":
        kwargs["os"] = "windows"
    if proxy:
        kwargs["proxy"] = proxy
    last_err = None
    geos = (True, False) if proxy else (False,)
    for geo in geos:
        kwargs["geoip"] = geo
        log("启动 Camoufox geoip=%s" % geo)
        fox = None
        try:
            fox = Camoufox(**kwargs)
            browser = fox.__enter__()
            page = browser.new_page()
            return ("camoufox", fox, browser, page)
        except Exception as e:
            last_err = e
            log("Camoufox 启动失败 geoip=%s: %s" % (geo, e))
            if fox is not None:
                try:
                    fox.__exit__(None, None, None)
                except Exception:
                    pass
    raise last_err or RuntimeError("Camoufox 启动失败")


def launch_playwright(job: Dict[str, Any]):
    from playwright.sync_api import sync_playwright

    proxy = parse_proxy(job.get("proxy"))
    pw = sync_playwright().start()
    launch_kwargs = {"headless": bool(job.get("headless"))}
    if proxy:
        launch_kwargs["proxy"] = proxy
    browser = pw.chromium.launch(**launch_kwargs)
    context = browser.new_context(
        viewport={"width": 1365, "height": 900},
        locale="en-US",
        ignore_https_errors=True,
    )
    page = context.new_page()
    return ("playwright", pw, browser, page)


def close_browser(kind: str, owner, browser) -> None:
    try:
        if kind == "camoufox":
            owner.__exit__(None, None, None)
        else:
            browser.close()
            owner.stop()
    except Exception:
        pass


def run_signup(job: Dict[str, Any], bridge: Bridge) -> None:
    engine = (job.get("engine") or "camoufox").lower()
    emit({"type": "status", "step": "launch", "engine": engine})
    kind = engine
    owner = None
    browser = None
    page = None
    try:
        if engine == "playwright":
            kind, owner, browser, page = launch_playwright(job)
        else:
            kind, owner, browser, page = launch_camoufox(job)
    except ImportError as e:
        fail("浏览器引擎未安装: %s" % e)
        return
    except Exception as e:
        fail("启动浏览器失败: %s" % e)
        return

    email = job.get("email") or ""
    password = job.get("password") or ""
    full_name = job.get("full_name") or "Alex Chen"
    device_url = job.get("device_url") or ""
    user_code = str(job.get("user_code") or "")
    otp_timeout = float(job.get("otp_timeout_sec") or 180)
    overall = float(job.get("overall_timeout_sec") or 600)

    email_filled = False
    otp_requested = False
    otp_filled = False
    password_filled = False
    name_filled = False
    allowed = False
    user_code_filled = False
    name_dumped = False
    signup_error_tries = 0

    try:
        log("打开设备授权页")
        attach_network(page)
        page.goto(device_url, wait_until="domcontentloaded", timeout=90000)
        time.sleep(2.0)
        try:
            page.wait_for_selector("input, button", timeout=20000)
        except Exception:
            pass
        emit({"type": "status", "step": "open_device"})
        deadline = time.time() + overall
        last_url = ""
        while time.time() < deadline:
            if bridge.cancelled():
                snapshot(page, "cancelled")
                fail("任务已取消")
                return
            try:
                url = page.url or ""
            except Exception:
                url = last_url
            text = body_text(page)
            if "enable javascript" in text.lower() and len(text) < 280:
                time.sleep(1.2)
                continue
            if looks_signup_error(text):
                snapshot(page, "signup-error")
                signup_error_tries += 1
                log("注册页错误 (%d): %s" % (signup_error_tries, text_snippet(text)))
                if signup_error_tries >= 2:
                    fail("注册页返回错误: %s" % text_snippet(text))
                    return
                try:
                    page.keyboard.press("Escape")
                except Exception:
                    pass
                click_named(page, ("close", "dismiss", "try again", "重试", "关闭"), 800)
                time.sleep(2.5)
                click_named(page, NEXT_HINTS)
                time.sleep(2.0)
                continue
            if url != last_url:
                log("页面: %s" % url[:180])
                snip = text_snippet(text)
                if snip:
                    log("文案: %s" % snip)
                last_url = url
                emit({"type": "status", "step": "navigate", "url": url[:240]})

            if looks_blocked(text):
                snapshot(page, "blocked")
                fail("注册被拦截: TES 拒绝了本次请求")
                return
            if looks_success(text, url) and (otp_filled or password_filled or allowed):
                emit({"type": "device_authorized"})
                emit({"type": "done", "result": "authorized"})
                return

            dismiss_cookies(page)

            if not user_code_filled and user_code:
                if fill_user_code(page, user_code):
                    user_code_filled = True
                    log("已填写设备码")
                    time.sleep(random.uniform(0.3, 0.8))
                    click_named(page, NEXT_HINTS + ("confirm", "verify"))
                    time.sleep(random.uniform(0.8, 1.5))
                    continue

            if contains_any(text, EXISTING_HINTS) and contains_any(text, ("password", "密码")) and not otp_requested:
                snapshot(page, "existing")
                fail("邮箱已注册过，跳过")
                return

            if not email_filled and contains_any(text, EMAIL_HINTS):
                if fill_email(page, email):
                    email_filled = True
                    log("已填写邮箱")
                    time.sleep(random.uniform(3.0, 5.0))
                    click_named(page, NEXT_HINTS + ("sign in", "log in", "登录"))
                    time.sleep(random.uniform(1.2, 2.0))
                    continue

            if contains_any(text, CREATE_HINTS):
                if click_named(page, (
                    "create a builder id",
                    "create aws builder",
                    "create account",
                    "sign up",
                    "创建 aws builder",
                    "创建账户",
                    "注册",
                )):
                    log("进入 Builder ID 注册")
                    time.sleep(random.uniform(0.8, 1.5))
                    continue

            if contains_any(text, OTP_HINTS) and not otp_filled:
                if not otp_requested:
                    emit({"type": "need_otp", "email": email})
                    emit({"type": "status", "step": "wait_otp"})
                    log("等待邮箱验证码")
                    otp_requested = True
                    code = bridge.wait_otp(otp_timeout)
                    if bridge.cancelled():
                        snapshot(page, "cancelled")
                        fail("任务已取消")
                        return
                    if not code:
                        snapshot(page, "otp-timeout")
                        fail("验证码接收超时")
                        return
                    if fill_otp(page, code):
                        otp_filled = True
                        log("已提交验证码")
                        time.sleep(random.uniform(0.5, 1.2))
                        click_named(page, NEXT_HINTS + ("verify", "confirm", "验证"))
                        continue
                    snapshot(page, "otp-input")
                    fail("未找到验证码输入框")
                    return

            if contains_any(text, NAME_HINTS) and not name_filled:
                ready = first_visible_any(page, [
                    'input[placeholder*="Silva"]',
                    'input[placeholder*="Maria"]',
                    'input[id^="formField"]',
                ], 2000)
                if ready is None:
                    time.sleep(0.8)
                    continue
                time.sleep(random.uniform(2.5, 4.0))
                if fill_name(page, full_name):
                    log("已填写姓名")
                    time.sleep(random.uniform(6.0, 9.0))
                    click_named(page, NEXT_HINTS)
                    time.sleep(random.uniform(2.0, 3.0))
                    later = body_text(page)
                    if contains_any(later, NAME_HINTS) and "required" in later.lower():
                        log("姓名未生效，将重试")
                    else:
                        name_filled = True
                    continue
                if not name_dumped:
                    snapshot(page, "name-page")
                    dump_inputs(page)
                    log("未找到姓名输入框")
                    name_dumped = True
                time.sleep(1.0)
                continue

            if contains_any(text, PASSWORD_HINTS) and not password_filled:
                if fill_passwords(page, password):
                    password_filled = True
                    log("已填写密码")
                    time.sleep(random.uniform(0.4, 1.0))
                    click_named(page, NEXT_HINTS + ("create", "sign up", "注册", "完成"))
                    continue

            if contains_any(text, ALLOW_HINTS) and (otp_filled or password_filled or email_filled):
                if click_named(page, ALLOW_HINTS):
                    allowed = True
                    log("已确认设备授权")
                    time.sleep(1.5)
                    continue

            need_field = (
                (contains_any(text, NAME_HINTS) and not name_filled)
                or (contains_any(text, OTP_HINTS) and not otp_filled)
                or (contains_any(text, PASSWORD_HINTS) and not password_filled)
            )
            if not need_field and click_named(page, ("next", "continue", "下一步", "继续"), 400):
                time.sleep(random.uniform(0.6, 1.2))
                continue

            time.sleep(0.9)

        snapshot(page, "timeout")
        fail("浏览器注册超时")
    except Exception as e:
        snapshot(page, "exception")
        fail("浏览器自动化异常: %s" % e)
    finally:
        close_browser(kind, owner, browser)


def run_check(engine: str) -> int:
    engine = (engine or "camoufox").lower()
    try:
        if engine == "playwright":
            from playwright.sync_api import sync_playwright  # noqa: F401
        else:
            import camoufox  # noqa: F401
        emit({"type": "check", "ok": True, "engine": engine})
        return 0
    except Exception as e:
        emit({"type": "check", "ok": False, "engine": engine, "error": str(e)})
        return 1


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--job", help="path to job JSON")
    parser.add_argument("--check", help="check engine import (camoufox|playwright)")
    args = parser.parse_args()
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

    # Playwright/Camoufox 会往 stdout 打进度条；协议通道必须独占 stdout，否则管道会堵住。
    _redirect_stdio_for_browser()

    if args.check:
        return run_check(args.check)

    if not args.job:
        fail("缺少 --job")
        return 1
    try:
        with open(args.job, "r", encoding="utf-8") as f:
            job = json.load(f)
    except Exception as e:
        fail("读取任务文件失败: %s" % e)
        return 1

    # Numpy/OpenBLAS DllMain 和 stdin 读线程会在 Windows 上互锁，必须先导入引擎。
    engine = (job.get("engine") or "camoufox").lower()
    try:
        preload_engine(engine)
    except ImportError as e:
        fail("浏览器引擎未安装: %s" % e)
        return 1
    except Exception as e:
        fail("导入浏览器引擎失败: %s" % e)
        return 1

    bridge = Bridge()
    run_signup(job, bridge)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
