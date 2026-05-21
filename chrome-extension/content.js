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
// content script 继承页面（HTTP）上下文，无法直接访问 localhost；
// background service worker 运行在扩展特权上下文，不受此限制。
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

async function startReview({ projectId, mrIID }) {
  const data = await chrome.storage.sync.get(['serviceUrl', 'apiKey', 'gitlabToken']);
  const cfg = { ...data, serviceUrl: data.serviceUrl || 'http://10.20.21.119:8082' };

  if (!cfg.serviceUrl) {
    showPanel('⚠️ 请先在插件设置中配置审查服务地址。', true);
    return;
  }

  const btn = document.getElementById('cr-review-btn');
  if (btn) { btn.disabled = true; btn.textContent = '⏳ 审查中...'; }
  showPanel('正在提交审查请求，请稍候...', false);

  try {
    const headers = { 'Content-Type': 'application/json' };
    if (cfg.apiKey) headers['X-API-Key'] = cfg.apiKey;

    const raw = await bgFetch(
      `${cfg.serviceUrl}/api/review`,
      'POST',
      headers,
      JSON.stringify({
        project_id: projectId,
        mr_iid: mrIID,
        gitlab_token: cfg.gitlabToken || undefined,
      })
    );

    const { job_id } = JSON.parse(raw);
    showPanel('AI 正在审查代码，通常需要 20-60 秒...', false);
    pollResult(cfg, job_id);
  } catch (e) {
    showPanel(`❌ 触发失败：${e.message}`, true);
    resetBtn();
  }
}

async function pollResult(cfg, jobId) {
  const headers = {};
  if (cfg.apiKey) headers['X-API-Key'] = cfg.apiKey;

  const poll = async () => {
    try {
      const raw = await bgFetch(`${cfg.serviceUrl}/api/review/${jobId}`, 'GET', headers);
      const data = JSON.parse(raw);

      if (data.status === 'done') {
        showPanel(data.result, false, true);
        resetBtn();
      } else if (data.status === 'failed') {
        showPanel(`❌ 审查失败：${data.error}`, true);
        resetBtn();
      } else {
        setTimeout(poll, 2000); // pending，继续轮询
      }
    } catch (e) {
      showPanel(`❌ 查询失败：${e.message}`, true);
      resetBtn();
    }
  };
  setTimeout(poll, 2000);
}

function resetBtn() {
  const btn = document.getElementById('cr-review-btn');
  if (btn) { btn.disabled = false; btn.textContent = '🤖 AI 代码审查'; }
}

function showPanel(content, isError, isMarkdown = false) {
  let panel = document.getElementById('cr-result-panel');
  if (!panel) {
    panel = document.createElement('div');
    panel.id = 'cr-result-panel';
    const container = document.querySelector('.content-wrapper main, .main-content, #content-body');
    if (container) container.prepend(panel);
    else document.body.prepend(panel);
  }

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
