'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

function makeElement(tag) {
  const children = [];
  const attrs = {};
  const listeners = {};
  let text = '';
  const el = {
    tagName: tag.toUpperCase(),
    children,
    dataset: {},
    setAttribute(k, v) { attrs[k] = String(v); },
    getAttribute(k) { return attrs[k] || null; },
    hasAttribute(k) { return k in attrs; },
    removeAttribute(k) { delete attrs[k]; },
    appendChild(child) {
      child.parent = el;
      children.push(child);
      return child;
    },
    insertBefore(child, ref) {
      child.parent = el;
      const idx = children.indexOf(child);
      if (idx >= 0) children.splice(idx, 1);
      const refIdx = ref ? children.indexOf(ref) : -1;
      if (refIdx >= 0) children.splice(refIdx, 0, child);
      else children.push(child);
      return child;
    },
    remove() {
      if (this.parent) {
        const i = this.parent.children.indexOf(this);
        if (i >= 0) this.parent.children.splice(i, 1);
      }
    },
    querySelector(sel) { return makeElement('div'); },
    querySelectorAll(sel) { return children.filter(c => c.dataset && c.dataset.taskId); },
    addEventListener(evt, fn) { listeners[evt] = fn; },
    dispatchEvent(evt) { if (listeners[evt.type]) listeners[evt.type](evt); },
    closest(sel) { return el; },
    set textContent(v) { text = String(v); children.length = 0; },
    get textContent() { return text; },
  };
  return el;
}

const tbody = makeElement('tbody');
tbody.id = 'task-table-body';

const thead = makeElement('thead');
thead.id = 'task-table-head';

const thPrio = makeElement('th');
thPrio.className = 'sortable';
thPrio.dataset.col = 'priority';

const els = {
  'task-table-body': tbody,
  'task-table-head': thead,
  'queue-count': makeElement('span'),
};

const document = {
  activeElement: null,
  getElementById: id => els[id] || null,
  querySelectorAll: sel => (sel.includes('th') ? [thPrio] : []),
  querySelector: () => null,
  addEventListener() {},
  createElement: makeElement,
};

const requestedURLs = [];
const fetchStub = async (url) => {
  requestedURLs.push(url);
  return {
    ok: true,
    status: 200,
    headers: { get: () => 'application/json' },
    text: async () => '[]',
    json: async () => [],
  };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { setupTableSorting, sort };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  api.setupTableSorting();

  await api.sort('priority');
  const url1 = requestedURLs[requestedURLs.length - 1];

  await api.sort('priority');
  const url2 = requestedURLs[requestedURLs.length - 1];

  await api.sort('claim_count');
  const url3 = requestedURLs[requestedURLs.length - 1];

  process.stdout.write(JSON.stringify({ url1, url2, url3 }));
})();
