// Background Service Worker 在扩展特权上下文运行，不受 PNA 限制，
// 可以代 content script 访问 localhost。

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg.type === 'FETCH') {
    handleFetch(msg).then(sendResponse).catch(err => sendResponse({ ok: false, error: err.message }));
    return true; // 保持 sendResponse 通道开放（异步响应必须返回 true）
  }
});

async function handleFetch({ url, method = 'GET', headers = {}, body }) {
  const options = { method, headers };
  if (body) options.body = body;

  try {
    const resp = await fetch(url, options);
    const text = await resp.text();
    return { ok: resp.ok, status: resp.status, body: text };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}
