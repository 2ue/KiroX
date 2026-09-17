// ===== 浏览器注册（Camoufox / Playwright，与协议注册独立） =====
var _brEngine = 'camoufox';
var _brEmailProvider = 'mailalias';
var _brLogs = [];
var _brPrevRunning = false;
var _brEngineCache = null;

function _brT(key, fallback) {
  if (window.I18N && typeof window.I18N.t === 'function') {
    var v = window.I18N.t(key);
    if (v && v !== key) return v;
  }
  return fallback || key;
}

function selectBrowserEngine(engine) {
  _brEngine = engine === 'playwright' ? 'playwright' : 'camoufox';
  document.querySelectorAll('.br-engine-pill').forEach(function(el) {
    el.classList.toggle('active', el.getAttribute('data-engine') === _brEngine);
  });
  renderBrowserEngineStatus(_brEngineCache);
}

function onBrowserEmailProvider(value) {
  _brEmailProvider = value || 'mailalias';
  var fields = document.getElementById('br-mailalias-fields');
  if (fields) fields.style.display = _brEmailProvider === 'mailalias' ? 'grid' : 'none';
}

async function loadBrowserMailAliasDefaults() {
  var urlEl = document.getElementById('br-mailalias-url');
  var prefixEl = document.getElementById('br-mailalias-prefix');
  if (!urlEl) return;
  try {
    var cfg = await window.go.main.App.GetMailAliasConfig() || {};
    if (!urlEl.value) urlEl.value = cfg.baseUrl || 'http://43.165.0.86:63888';
    if (prefixEl && !prefixEl.value) prefixEl.value = cfg.prefix || '';
  } catch (e) {
    if (!urlEl.value) urlEl.value = 'http://43.165.0.86:63888';
  }
}

function renderBrowserEngineStatus(st) {
  _brEngineCache = st || {};
  var label = document.getElementById('br-python-label');
  var pythonDot = document.getElementById('br-dot-python');
  var installBtn = document.getElementById('br-btn-install');
  if (!label) return;

  var ready = false;
  var text = _brT('browser.checking', '正在检测引擎…');
  if (st && st.installing) {
    text = _brT('browser.installing', '正在安装引擎…') + (st.installEngine ? ' (' + st.installEngine + ')' : '');
  } else if (!st || !st.pythonFound) {
    text = _brT('browser.pythonMissing', '未找到 Python 3，请先安装 Python 3.10+');
  } else {
    var engineReady = _brEngine === 'playwright' ? st.playwrightReady : st.camoufoxReady;
    var engineErr = _brEngine === 'playwright' ? st.playwrightError : st.camoufoxError;
    if (engineReady) {
      ready = true;
      text = 'Python ' + (st.pythonVersion || '') + ' · ' + (_brEngine === 'playwright' ? 'Playwright' : 'Camoufox') + ' ' + _brT('browser.ready', '已就绪');
    } else {
      text = (_brEngine === 'playwright' ? 'Playwright' : 'Camoufox') + ' ' + _brT('browser.notReady', '未安装') + (engineErr ? ' — ' + engineErr : '');
    }
  }
  label.textContent = text;
  if (pythonDot) pythonDot.classList.toggle('ok', !!(st && st.pythonFound));
  if (installBtn) installBtn.disabled = !!(st && st.installing);
}

async function refreshBrowserEngine() {
  var label = document.getElementById('br-python-label');
  if (label) label.textContent = _brT('browser.checking', '正在检测引擎…');
  try {
    var st = await window.go.main.App.GetBrowserSignupEngine();
    renderBrowserEngineStatus(st);
  } catch (e) {
    renderBrowserEngineStatus({ pythonFound: false, camoufoxError: e.message || String(e) });
  }
}

async function installBrowserEngine() {
  try {
    var result = await window.go.main.App.InstallBrowserSignupEngine(_brEngine);
    if (result && result.error) {
      showToast(result.error, 'error');
      return;
    }
    showToast(_brT('browser.installStarted', '已开始安装引擎，请查看下方日志'), 'success');
    pollBrowserSignup();
  } catch (e) {
    showToast(_brT('browser.installFailed', '安装失败') + ': ' + (e.message || e), 'error');
  }
}

function collectBrowserSignupConfig() {
  var cfg = {
    engine: _brEngine,
    headless: !!(document.getElementById('br-headless') && document.getElementById('br-headless').checked),
    count: parseInt((document.getElementById('br-count') || {}).value, 10) || 1,
    emailProvider: _brEmailProvider || 'mailalias',
    proxy: ((document.getElementById('br-proxy-select') || {}).dataset || {}).value || '',
    proxyConfigured: true
  };
  if (cfg.emailProvider === 'mailalias') {
    cfg.mailAliasConfig = {
      baseUrl: ((document.getElementById('br-mailalias-url') || {}).value || '').trim(),
      prefix: ((document.getElementById('br-mailalias-prefix') || {}).value || '').trim(),
      mode: 'mixed',
      length: 8
    };
    if (!cfg.mailAliasConfig.baseUrl) {
      throw new Error(_brT('mailalias.requiredUrl', '请填写 API URL'));
    }
  }
  return cfg;
}

async function startBrowserSignup() {
  try {
    var cfg = collectBrowserSignupConfig();
    var result = await window.go.main.App.StartBrowserSignup(cfg);
    if (result && result.error) {
      showToast(result.error, 'error');
      return;
    }
    showToast(_brT('browser.started', '浏览器注册已启动'), 'success');
    pollBrowserSignup();
  } catch (e) {
    showToast(_brT('toast.taskStartFailed', '启动失败') + ': ' + (e.message || e), 'error');
  }
}

async function stopBrowserSignup() {
  try {
    var result = await window.go.main.App.StopBrowserSignup();
    if (result && result.error) {
      showToast(result.error, 'error');
      return;
    }
    showToast(_brT('toast.taskStopping', '正在停止任务...'));
  } catch (e) {
    showToast(_brT('toast.taskStopFailed', '停止失败') + ': ' + (e.message || e), 'error');
  }
}

function renderBrowserLogs() {
  var box = document.getElementById('br-log-box');
  if (!box) return;
  var wasAtBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 50;
  var logs = _brLogs || [];
  var html;
  if (!logs.length) {
    html = '<span style="color:var(--text-muted);">' + _brT('logs.empty', '暂无日志') + '</span>';
  } else if (typeof _formatLogLine === 'function') {
    html = logs.map(function(l) { return _formatLogLine(String(l).replace(/^\s+/, '')); }).join('\n');
  } else {
    html = logs.map(function(l) { return _escapeLogHtml ? _escapeLogHtml(String(l)) : String(l); }).join('\n');
  }
  if (box.innerHTML !== html) {
    box.innerHTML = html;
    if (wasAtBottom) box.scrollTop = box.scrollHeight;
  }
}

function copyBrowserLogs() {
  var logs = _brLogs || [];
  if (!logs.length) {
    showToast(_brT('toast.logEmpty', '暂无日志可复制'), 'error');
    return;
  }
  navigator.clipboard.writeText(logs.join('\n')).then(function() {
    showToast(_brT('toast.logCopied', '日志已复制到剪贴板'), 'success');
  }).catch(function(e) {
    showToast(_brT('toast.copyFailed', '复制失败') + ': ' + e.message, 'error');
  });
}

function setBrowserRunning(running) {
  var startBtn = document.getElementById('br-btn-start');
  var stopBtn = document.getElementById('br-btn-stop');
  if (startBtn) startBtn.disabled = !!running;
  if (stopBtn) stopBtn.disabled = !running;
}

async function pollBrowserSignup() {
  try {
    var s = await window.go.main.App.GetBrowserSignupStatus();
    var running = !!(s && (s.running || s.installing));
    setBrowserRunning(!!(s && s.running));
    var progress = document.getElementById('br-progress');
    if (progress) progress.textContent = (s.completed || 0) + '/' + (s.total || 0);
    var ok = document.getElementById('br-success');
    if (ok) ok.textContent = String(s.success || 0);
    var fail = document.getElementById('br-failed');
    if (fail) fail.textContent = String(s.failed || 0);
    var elapsed = document.getElementById('br-elapsed');
    if (elapsed && typeof formatTime === 'function') elapsed.textContent = s.elapsed > 0 ? formatTime(s.elapsed) : '0s';
    if (_brPrevRunning && s && !s.running && s.completed > 0 && typeof notifyTaskComplete === 'function') {
      notifyTaskComplete(_brT('browser.title', '浏览器注册'), s.success, s.failed, s.completed);
    }
    _brPrevRunning = !!(s && s.running);
    if (s && s.installing) {
      renderBrowserEngineStatus(Object.assign({}, _brEngineCache || {}, s));
    } else if (_brPrevRunning === false && _brEngineCache && _brEngineCache.installing) {
      refreshBrowserEngine();
    }
  } catch (e) {}
  try {
    _brLogs = await window.go.main.App.GetBrowserSignupLogs() || [];
    renderBrowserLogs();
  } catch (e) {}
}

function initBrowserPage() {
  var provider = document.getElementById('br-email-provider');
  if (provider && typeof setDropdownValue === 'function') {
    setDropdownValue(provider, _brEmailProvider);
  }
  onBrowserEmailProvider(_brEmailProvider);
  if (typeof loadProxyOptions === 'function') loadProxyOptions();
  loadBrowserMailAliasDefaults();
  refreshBrowserEngine();
  pollBrowserSignup();
}

setInterval(function() {
  if (typeof _currentPageId !== 'undefined' && _currentPageId === 'browser') {
    pollBrowserSignup();
  }
}, 1500);

window.addEventListener('i18n:changed', function() {
  if (typeof _currentPageId !== 'undefined' && _currentPageId === 'browser') {
    renderBrowserLogs();
    renderBrowserEngineStatus(_brEngineCache);
  }
});
