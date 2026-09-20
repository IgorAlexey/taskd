'use strict';
const fs = require('fs');

function makeElement(tag) {
  const children = [];
  const attrs = {};
  const listeners = {};
  let text = '';
  const el = {
    tagName: tag.toUpperCase(),
    children,
    options: children,
    selectedIndex: 0,
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
    querySelector(sel) { return null; },
    querySelectorAll(sel) { return []; },
    addEventListener(evt, fn) { listeners[evt] = fn; },
    dispatchEvent(evt) { if (listeners[evt.type]) listeners[evt.type](evt); },
    closest(sel) {
      if (sel.includes('th.sortable')) {
        return (el.className && el.className.includes('sortable')) ? el : null;
      }
      return el;
    },
    get value() {
      if (tag.toLowerCase() === 'select') {
        const opt = children[el.selectedIndex];
        return opt ? opt.value : '';
      }
      return text;
    },
    set value(v) {
      if (tag.toLowerCase() === 'select') {
        const idx = children.findIndex(o => o.value === v);
        el.selectedIndex = idx >= 0 ? idx : 0;
      } else {
        text = String(v);
      }
    },
    set textContent(v) { text = String(v); children.length = 0; },
    get textContent() { return text; },
  };
  return el;
}

function setupHarness(htmlPath, initialSearch = '') {
  const src = fs.readFileSync(htmlPath, 'utf8');
  const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

  const tbody = makeElement('tbody');
  tbody.id = 'task-table-body';

  const thead = makeElement('thead');
  thead.id = 'task-table-head';

  const cols = ['id', 'project', 'status', 'priority', 'claim_count', 'worker'];
  const headers = {};
  for (const col of cols) {
    const th = makeElement('th');
    th.className = 'sortable';
    th.dataset.col = col;
    thead.appendChild(th);
    headers[col] = th;
  }

  const els = {
    'task-table-body': tbody,
    'task-table-head': thead,
    'queue-count': makeElement('span'),
    'filter-project': makeElement('select'),
    'filter-priority': makeElement('input'),
    'filter-search': makeElement('input'),
    'filter-worker': makeElement('select'),
    'form-project': makeElement('input'),
    'form-project-list': makeElement('datalist'),
  };

  const allThs = Object.values(headers);
  const document = {
    activeElement: null,
    getElementById: id => els[id] || null,
    querySelectorAll: sel => (sel.includes('th') ? allThs : []),
    querySelector: sel => null,
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

  const loc = { pathname: '/ui', search: initialSearch, hash: '' };
  const replacedStates = [];
  const historyObj = {
    pushState(state, title, url) {
      const qIdx = url.indexOf('?');
      loc.search = qIdx >= 0 ? url.slice(qIdx) : '';
    },
    replaceState(state, title, url) {
      replacedStates.push({ state, title, url });
      const qIdx = url.indexOf('?');
      loc.search = qIdx >= 0 ? url.slice(qIdx) : '';
    },
  };

  const api = new Function(
    'document', 'location', 'history', 'window', 'fetch', 'console',
    'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
    script + '\nreturn { setupTableSorting, sort, applyURLState, readURLState, currentURLState, syncURL, loadTasks };'
  )(
    document, loc,
    historyObj, { addEventListener() {} },
    fetchStub, { log() {}, error() {} },
    () => 0, () => {}, () => 1, () => {},
    Date,
    { timeout: () => new AbortController().signal }
  );

  return { api, headers, location: loc, requestedURLs, replacedStates };
}

if (require.main === module) {
  (async () => {
    const { api, requestedURLs } = setupHarness(process.argv[2]);
    api.setupTableSorting();

    await api.sort('priority');
    const url1 = requestedURLs[requestedURLs.length - 1];

    await api.sort('priority');
    const url2 = requestedURLs[requestedURLs.length - 1];

    await api.sort('claim_count');
    const url3 = requestedURLs[requestedURLs.length - 1];

    process.stdout.write(JSON.stringify({ url1, url2, url3 }));
  })();
}

module.exports = { setupHarness, makeElement };
