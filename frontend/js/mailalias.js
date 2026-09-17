// ===== MailAlias (Gmail 临时邮箱) 配置管理 =====
const DEFAULT_MAILALIAS_BASE_URL = 'http://43.165.0.86:63888';
let mailaliasConfig = {};

function collectMailAliasFormConfig() {
  var length = parseInt(document.getElementById('mailalias-inline-length').value, 10);
  if (!Number.isFinite(length)) length = 8;
  return {
    baseUrl: document.getElementById('mailalias-inline-url').value.trim(),
    prefix: document.getElementById('mailalias-inline-prefix').value.trim(),
    mode: document.getElementById('mailalias-inline-mode').value || 'mixed',
    length: length
  };
}

async function inlineTestMailAlias() {
  var config = collectMailAliasFormConfig();
  if (!config.baseUrl) {
    showToast(_mmT('mailalias.requiredUrl', '请填写 API URL'), 'error');
    return;
  }
  var btn = document.getElementById('mailalias-inline-test-btn');
  var statusEl = document.getElementById('mailalias-inline-status');
  setInlineTestButton(btn, true, 'moemail.testing');
  try {
    var result = await window.go.main.App.TestMailAliasConnection(JSON.stringify(config));
    if (result && result.success) {
      setLocalizedStatus(statusEl, 'mailalias.testOk', { n: (result.alias || '') }, '连接成功，示例 {n}');
    } else {
      clearLocalizedStatus(statusEl, result && result.error ? result.error : _mmT('moemail.testFailed', '连接失败'));
    }
  } catch (e) {
    clearLocalizedStatus(statusEl, e.message || String(e));
  } finally {
    setInlineTestButton(btn, false, 'moemail.testing');
  }
}

async function inlineSaveMailAlias() {
  var config = collectMailAliasFormConfig();
  if (!config.baseUrl) {
    showToast(_mmT('mailalias.requiredUrl', '请填写 API URL'), 'error');
    return;
  }
  var btn = document.getElementById('mailalias-inline-test-btn');
  var statusEl = document.getElementById('mailalias-inline-status');
  setInlineTestButton(btn, true, 'moemail.testing');
  var testResult;
  try {
    testResult = await window.go.main.App.TestMailAliasConnection(JSON.stringify(config));
  } catch (e) {
    testResult = { error: e.message || String(e) };
  } finally {
    setInlineTestButton(btn, false, 'moemail.testing');
  }
  if (!testResult || !testResult.success) {
    var err = (testResult && testResult.error) || _mmT('moemail.testFailed', '连接失败');
    clearLocalizedStatus(statusEl, err);
    showToast(_mmT('moemail.cannotSaveUntilOk', '连接测试未通过，未保存配置: ') + err, 'error');
    return;
  }
  mailaliasConfig = config;
  const saveResult = await window.go.main.App.SaveMailAliasConfig(JSON.stringify(mailaliasConfig));
  if (saveResult && saveResult.error) {
    showToast(saveResult.error, 'error');
    return;
  }
  setLocalizedStatus(statusEl, 'mailalias.testOk', { n: (testResult.alias || '') }, '连接成功，示例 {n}');
  updateMailAliasUI();
  showToast(_mmT('mailalias.saved', 'Gmail 临时邮箱配置已保存'), 'success');
}

async function loadMailAliasConfig() {
  try {
    mailaliasConfig = await window.go.main.App.GetMailAliasConfig() || {};
    updateMailAliasUI();
  } catch (e) {
    console.error('[MailAlias] 加载配置失败:', e);
    mailaliasConfig = {};
    updateMailAliasUI();
  }
}

function updateMailAliasUI() {
  const summaryEl = document.getElementById('settings-mailalias-summary');
  const urlEl = document.getElementById('mailalias-inline-url');
  const prefixEl = document.getElementById('mailalias-inline-prefix');
  const modeEl = document.getElementById('mailalias-inline-mode');
  const lengthEl = document.getElementById('mailalias-inline-length');
  const configured = mailaliasConfig && mailaliasConfig.baseUrl;
  if (urlEl) urlEl.value = configured ? mailaliasConfig.baseUrl : DEFAULT_MAILALIAS_BASE_URL;
  if (prefixEl) prefixEl.value = (mailaliasConfig && mailaliasConfig.prefix) || '';
  if (modeEl) modeEl.value = (mailaliasConfig && mailaliasConfig.mode) || 'mixed';
  if (lengthEl) lengthEl.value = (mailaliasConfig && mailaliasConfig.length) || 8;
  if (summaryEl) {
    summaryEl.textContent = configured
      ? _mmT('mailalias.summaryActive', '已配置')
      : _mmT('mailalias.summaryNone', '未配置');
  }
}

document.addEventListener('DOMContentLoaded', async function () {
  await loadMailAliasConfig();
});

window.addEventListener('i18n:changed', function () {
  try {
    if (typeof updateMailAliasUI === 'function') updateMailAliasUI();
  } catch (e) {}
  var btn = document.getElementById('mailalias-inline-test-btn');
  if (btn) setInlineTestButton(btn, btn.disabled, 'moemail.testing');
});
