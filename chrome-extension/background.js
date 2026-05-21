// Background Service Worker 在扩展特权上下文运行，不受 PNA 限制，
// 可以代 content script 访问 localhost。

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.type === 'FETCH') {
    handleFetch(msg).then(sendResponse).catch(err => sendResponse({ ok: false, error: err.message }));
    return true; // 保持 sendResponse 通道开放（异步响应必须返回 true）
  }
  if (msg.type === 'STREAM') {
    const tabId = sender.tab?.id;
    if (!tabId) { sendResponse({ ok: false, error: '无法获取标签页 ID' }); return; }
    handleStream(msg, tabId).catch(err => {
      chrome.tabs.sendMessage(tabId, { type: 'STREAM_ERROR', error: err.message });
    });
    sendResponse({ ok: true }); // 立即确认，后续通过 tabs.sendMessage 推送
    return true;
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

// handleStream 发起 SSE 请求，逐行解析事件并通过 tabs.sendMessage 推送给 content script。
async function handleStream({ url, headers = {}, body }, tabId) {
  const resp = await fetch(url, { method: 'POST', headers, body });
  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(`请求失败 (${resp.status}): ${text}`);
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });

    // SSE 以 \n\n 分隔事件
    const parts = buf.split('\n\n');
    buf = parts.pop(); // 最后一段可能不完整，留待下次拼接

    for (const part of parts) {
      const lines = part.split('\n');
      let event = 'message';
      let data = '';
      for (const line of lines) {
        if (line.startsWith('event: ')) event = line.slice(7).trim();
        if (line.startsWith('data: ')) data = line.slice(6).trim();
      }
      if (!data) continue;

      let payload;
      try { payload = JSON.parse(data); } catch { payload = data; }

      if (event === 'token') {
        chrome.tabs.sendMessage(tabId, { type: 'STREAM_CHUNK', chunk: payload });
      } else if (event === 'done') {
        chrome.tabs.sendMessage(tabId, { type: 'STREAM_END' });
        return;
      } else if (event === 'error') {
        chrome.tabs.sendMessage(tabId, { type: 'STREAM_ERROR', error: payload });
        return;
      }
    }
  }
  // 响应体结束但未收到 done 事件，仍视为完成
  chrome.tabs.sendMessage(tabId, { type: 'STREAM_END' });
}
