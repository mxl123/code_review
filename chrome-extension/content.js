// 匹配 GitLab MR 详情页：/group/project/-/merge_requests/数字
const MR_URL_RE = /\/merge_requests\/(\d+)/;

function getMRInfo() {
  const match = location.pathname.match(MR_URL_RE);
  if (!match) return null;
  const mrIID = parseInt(match[1], 10);

  // 从 GitLab 全局变量获取 project_id（所有 GitLab 页面都有此变量）
  const projectId = window.gl?.data?.projectId
    || document.querySelector('body')?.dataset?.projectId;

  if (!projectId) return null;
  return { projectId: parseInt(projectId, 10), mrIID };
}

function injectButton() {
  if (document.getElementById('cr-review-btn')) return;

  const info = getMRInfo();
  if (!info) return;

  // 找 MR 标题区域，注入审查按钮
  const titleArea = document.querySelector('.detail-page-header-actions, .mr-widget-section, .page-content-header');
  if (!titleArea) return;

  const btn = document.createElement('button');
  btn.id = 'cr-review-btn';
  btn.textContent = '🤖 AI 代码审查';
  btn.className = 'cr-btn';
  btn.addEventListener('click', () => startReview(info));
  titleArea.appendChild(btn);
}

// bgFetch 通过 background service worker 发起请求，绕过 PNA 限制。
function bgFetch(url, method = 'GET', headers = {}, body) {
  return new Promise((resolve, reject) => {
    chrome.runtime.sendMessage(
      { type: 'FETCH', url, method, headers, body },
      (resp) => {
        if (chrome.runtime.lastError) {
          reject(new Error(chrome.runtime.lastError.message));
          return;
        }
        if (!resp.ok) {
          reject(new Error(resp.error || `请求失败 (${resp.status})`));
          return;
        }
        resolve(resp.body);
      }
    );
  });
}

// bgStream 通过 background service worker 发起 SSE 流式请求。
// 返回 Promise<void>，流事件通过 chrome.runtime.onMessage 推送回来。
function bgStream(url, headers = {}, body) {
  return new Promise((resolve, reject) => {
    chrome.runtime.sendMessage(
      { type: 'STREAM', url, headers, body },
      (resp) => {
        if (chrome.runtime.lastError) {
          reject(new Error(chrome.runtime.lastError.message));
          return;
        }
        if (!resp.ok) {
          reject(new Error(resp.error || '流式请求初始化失败'));
          return;
        }
        resolve();
      }
    );
  });
}

async function startReview({ projectId, mrIID }) {
  const data = await chrome.storage.sync.get(['serviceUrl', 'apiKey', 'gitlabToken']);
  const cfg = { ...data, serviceUrl: data.serviceUrl || 'http://10.20.21.119:8082' };

  if (!cfg.serviceUrl) {
    showPanel('⚠️ 请先在插件设置中配置审查服务地址。', true);
    return;
  }

  const btn = document.getElementById('cr-review-btn');
  if (btn) { btn.disabled = true; btn.textContent = '⏳ 审查中...'; }
  showPanel('正在连接审查服务，请稍候...', false);

  const headers = { 'Content-Type': 'application/json' };
  if (cfg.apiKey) headers['X-API-Key'] = cfg.apiKey;

  const body = JSON.stringify({
    project_id: projectId,
    mr_iid: mrIID,
    gitlab_token: cfg.gitlabToken || undefined,
  });

  // 注册流式消息监听器（在发起请求前注册，避免遗漏首批 chunk）
  let streamBuf = '';
  let firstChunk = true;

  const onStreamMsg = (msg) => {
    if (msg.type === 'STREAM_CHUNK') {
      if (firstChunk) {
        firstChunk = false;
        showStreamPanel(); // 首个 token 到达时切换到流式展示面板
      }
      streamBuf += msg.chunk;
      updateStreamPanel(streamBuf);
    } else if (msg.type === 'STREAM_END') {
      chrome.runtime.onMessage.removeListener(onStreamMsg);
      finalizeStreamPanel(streamBuf);
      resetBtn();
    } else if (msg.type === 'STREAM_ERROR') {
      chrome.runtime.onMessage.removeListener(onStreamMsg);
      showPanel(`❌ 审查失败：${msg.error}`, true);
      resetBtn();
    }
  };
  chrome.runtime.onMessage.addListener(onStreamMsg);

  try {
    await bgStream(`${cfg.serviceUrl}/api/review/stream`, headers, body);
    // bgStream resolve 仅代表请求已发出，实际数据通过 onStreamMsg 接收
  } catch (e) {
    chrome.runtime.onMessage.removeListener(onStreamMsg);
    showPanel(`❌ 触发失败：${e.message}`, true);
    resetBtn();
  }
}

function resetBtn() {
  const btn = document.getElementById('cr-review-btn');
  if (btn) { btn.disabled = false; btn.textContent = '🤖 AI 代码审查'; }
}

// showStreamPanel 创建用于流式输出的面板（显示加载中状态）
function showStreamPanel() {
  let panel = getOrCreatePanel();
  panel.className = 'cr-panel';
  panel.innerHTML =
    `<div class="cr-panel-header"><span>🤖 AI 代码审查结果</span><button class="cr-close" id="cr-close-btn">✕</button></div>` +
    `<div class="cr-markdown" id="cr-stream-content"></div>`;
  panel.querySelector('#cr-close-btn').addEventListener('click', () => panel.remove());
}

// updateStreamPanel 在流式输出过程中实时渲染已接收内容
function updateStreamPanel(text) {
  const el = document.getElementById('cr-stream-content');
  if (el) el.innerHTML = renderMarkdown(text) + '<span class="cr-cursor">▌</span>';
}

// finalizeStreamPanel 流结束时移除光标，完成渲染
function finalizeStreamPanel(text) {
  const el = document.getElementById('cr-stream-content');
  if (el) el.innerHTML = renderMarkdown(text);
}

function getOrCreatePanel() {
  let panel = document.getElementById('cr-result-panel');
  if (!panel) {
    panel = document.createElement('div');
    panel.id = 'cr-result-panel';
    const container = document.querySelector('.content-wrapper main, .main-content, #content-body');
    if (container) container.prepend(panel);
    else document.body.prepend(panel);
  }
  return panel;
}

function showPanel(content, isError, isMarkdown = false) {
  const panel = getOrCreatePanel();
  panel.className = 'cr-panel' + (isError ? ' cr-panel-error' : '');

  const closeBtn = `<button class="cr-close" id="cr-close-btn">✕</button>`;
  if (isMarkdown) {
    panel.innerHTML = `<div class="cr-panel-header"><span>🤖 AI 代码审查结果</span>${closeBtn}</div><div class="cr-markdown">${renderMarkdown(content)}</div>`;
  } else {
    panel.innerHTML = `<div class="cr-panel-header"><span>🤖 AI 代码审查</span>${closeBtn}</div><div class="cr-msg">${content}</div>`;
  }
  panel.querySelector('#cr-close-btn').addEventListener('click', () => panel.remove());
}

// 页面加载完成及单页应用路由变化时注入按钮
injectButton();
const observer = new MutationObserver(injectButton);
observer.observe(document.body, { childList: true, subtree: true });
