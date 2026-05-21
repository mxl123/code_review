const $ = id => document.getElementById(id);

// 加载已保存的配置
chrome.storage.sync.get(['serviceUrl', 'apiKey', 'gitlabToken'], (data) => {
  if (data.serviceUrl)   $('serviceUrl').value   = data.serviceUrl;
  if (data.apiKey)       $('apiKey').value       = data.apiKey;
  if (data.gitlabToken)  $('gitlabToken').value  = data.gitlabToken;
});

$('save').addEventListener('click', () => {
  const serviceUrl  = $('serviceUrl').value.trim().replace(/\/$/, '');
  const apiKey      = $('apiKey').value.trim();
  const gitlabToken = $('gitlabToken').value.trim();

  chrome.storage.sync.set({ serviceUrl, apiKey, gitlabToken }, () => {
    const msg = $('savedMsg');
    msg.style.display = 'inline';
    setTimeout(() => { msg.style.display = 'none'; }, 2000);
  });
});
