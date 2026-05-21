// 轻量 Markdown 渲染器，覆盖代码审查场景中的常用语法。
function renderMarkdown(src) {
  // 第一步：抽取代码块，用占位符替换，避免其内容被后续规则处理
  const codeBlocks = [];
  src = src.replace(/```(\w*)\n?([\s\S]*?)```/g, (_, lang, code) => {
    const cls = lang ? ` class="language-${lang}"` : '';
    const escaped = code.trimEnd()
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    codeBlocks.push(`<pre><code${cls}>${escaped}</code></pre>`);
    return `\x00CODE${codeBlocks.length - 1}\x00`;
  });

  // 第二步：HTML 转义其余文本（占位符不含特殊字符，不受影响）
  src = src.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

  // 第三步：逐行处理块级元素
  const lines = src.split('\n');
  const output = [];
  let listItems = [];

  const flushList = () => {
    if (listItems.length) {
      output.push('<ul>' + listItems.map(t => `<li>${t}</li>`).join('') + '</ul>');
      listItems = [];
    }
  };

  for (const raw of lines) {
    const line = raw.trimEnd();

    if (/^### /.test(line))       { flushList(); output.push(`<h3>${inlineRender(line.slice(4))}</h3>`); }
    else if (/^## /.test(line))   { flushList(); output.push(`<h2>${inlineRender(line.slice(3))}</h2>`); }
    else if (/^# /.test(line))    { flushList(); output.push(`<h1>${inlineRender(line.slice(2))}</h1>`); }
    else if (/^[-*] /.test(line)) { listItems.push(inlineRender(line.slice(2))); }
    else if (/^---+$/.test(line)) { flushList(); output.push('<hr>'); }
    else if (line === '')          { flushList(); output.push(''); }
    else                           { flushList(); output.push(inlineRender(line)); }
  }
  flushList();

  // 第四步：将连续非块级行合并成 <p>
  const html = splitParagraphs(output.join('\n'));

  // 第五步：还原代码块占位符
  return html.replace(/\x00CODE(\d+)\x00/g, (_, i) => codeBlocks[i]);
}

// 将文本按空行分段，块级标签不包裹 <p>
function splitParagraphs(text) {
  const BLOCK_TAG = /^<(h[1-6]|ul|ol|pre|hr|blockquote)/;
  return text.split(/\n{2,}/).map(block => {
    block = block.trim();
    if (!block) return '';
    if (BLOCK_TAG.test(block) || block.startsWith('\x00CODE')) return block;
    return `<p>${block.replace(/\n/g, '<br>')}</p>`;
  }).join('\n');
}

// 处理行内语法：行内代码、粗体、斜体、链接
function inlineRender(text) {
  return text
    .replace(/`([^`]+)`/g,        '<code>$1</code>')
    .replace(/\*\*(.+?)\*\*/g,    '<strong>$1</strong>')
    .replace(/\*(.+?)\*/g,        '<em>$1</em>')
    .replace(/\[(.+?)\]\((.+?)\)/g, '<a href="$2" target="_blank">$1</a>');
}
