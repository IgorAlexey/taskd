'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

function makeEl(tag) {
  const children = [];
  const attrs = {};
  let text = '';
  return {
    tagName: tag.toUpperCase(),
    children,
    value: '',
    dataset: {},
    setAttribute(k, v) { attrs[k] = String(v); },
    getAttribute(k) { return attrs[k] || null; },
    hasAttribute(k) { return k in attrs; },
    appendChild(c) { children.push(c); return c; },
    addEventListener() {},
    querySelectorAll() { return []; },
    querySelector() { return null; },
    set textContent(v) { text = String(v); },
    get textContent() { return text; },
  };
}

const assetInput = makeEl('input');
assetInput.id = 'filter-asset-path';

const els = {
  'filter-asset-path': assetInput,
  'task-table-body': makeEl('tbody'),
  'task-table-head': makeEl('thead'),
  'queue-count': makeEl('span'),
};

const document = {
  activeElement: null,
  getElementById: id => els[id] || null,
  querySelectorAll: () => [],
  querySelector: () => null,
  addEventListener() {},
  createElement: makeEl,
};

let loc = { pathname: '/ui', search: '?asset_path=models%2Frobot.glb', hash: '' };
const history = {
  pushState(state, title, url) { loc.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
  replaceState(state, title, url) { loc.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
};

const requestedURLs = [];
const fetchStub = async (url) => {
  requestedURLs.push(url);
  return { ok: true, status: 200, headers: { get: () => 'application/json' }, text: async () => '[]', json: async () => [] };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { applyURLState, currentURLState, syncURL, loadTasks };'
)(
  document, loc, history, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  api.applyURLState();
  const initVal = assetInput.value;

  await api.loadTasks();
  const u1 = requestedURLs[requestedURLs.length - 1];

  assetInput.value = 'textures/skin.png';
  api.syncURL(false);
  const syncedSearch = loc.search;

  await api.loadTasks();
  const u2 = requestedURLs[requestedURLs.length - 1];

  process.stdout.write(JSON.stringify({ initVal, u1, syncedSearch, u2 }));
})();
