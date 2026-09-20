'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

function makeElement(tag) {
  const children = [];
  const attrs = {};
  let text = '';
  return {
    tagName: tag.toUpperCase(),
    children,
    dataset: {},
    setAttribute(k, v) { attrs[k] = String(v); },
    getAttribute(k) { return attrs[k] || null; },
    hasAttribute(k) { return k in attrs; },
    appendChild(child) { children.push(child); return child; },
    set textContent(v) { text = String(v); children.length = 0; },
    get textContent() { return text; },
  };
}

const document = {
  createElement: makeElement,
  addEventListener() {},
  getElementById: () => null,
  querySelector: () => null,
  querySelectorAll: () => [],
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { createRow };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  () => {}, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

const row = api.createRow({
  id: 'task-12345678',
  project: '<script>bad()</script>',
  status: 'pending',
  priority: 5,
  claim_count: 2,
  worker: 'worker<1>',
  summary: '<b>summary</b>',
}, false);

const cells = row.children;
const btn = cells[0].children[0];
const proj = cells[1];
const badge = cells[2].children[0];
const prio = cells[3];
const claim = cells[4];
const worker = cells[5];
const createdAt = cells[6];
const summary = cells[7];
const out = {
  btnTitle: btn.title,
  btnText: btn.textContent,
  projText: proj.textContent,
  projChildCount: proj.children.length,
  statusText: badge.textContent,
  statusAttr: badge.getAttribute('data-status'),
  prioText: prio.textContent,
  claimText: claim.textContent,
  workerText: worker.textContent,
  workerChildCount: worker.children.length,
  summaryText: summary.textContent,
  summaryChildCount: summary.children.length,
};

process.stdout.write(JSON.stringify(out, null, 2));
