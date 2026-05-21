const $ = id => document.getElementById(id);

const DEFAULTS = {
  serviceUrl: 'http://10.20.21.119:8082',
};

// 加载已保存的配置，未设置时使用默认值
chrome.storage.sync.get(['serviceUrl', 'apiKey', 'gitlabToken'], (data) => {
  $('serviceUrl').value  = data.serviceUrl  || DEFAULTS.serviceUrl;
  if (data.apiKey)       $('apiKey').value      = data.apiKey;
  if (data.gitlabToken)  $('gitlabToken').value = data.gitlabToken;
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
