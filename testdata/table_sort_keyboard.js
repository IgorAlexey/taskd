'use strict';
const fs = require('fs');
const html = fs.readFileSync(process.argv[2], 'utf8');

for (const th of html.match(/<th\b[^>]*class="[^"]*sortable[^"]*"[^>]*>/g) || []) {
  if (!th.includes('tabindex="0"')) {
    console.error('Missing tabindex="0" on', th);
    process.exit(1);
  }
}

const script = html.slice(html.indexOf('<script>') + 8, html.lastIndexOf('</script>'));
const listeners = {};
const thead = { id: 'task-table-head', addEventListener: (k, fn) => { listeners[k] = fn; } };
const doc = {
  getElementById: id => (id === 'task-table-head' ? thead : null),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

const sortState = { col: null };
const api = new Function('document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal', 'sortState',
  script + '\nsort = col => { sortState.col = col; };\nreturn { setupTableSorting };'
)(doc, { pathname: '/ui', search: '', hash: '' }, { pushState() {}, replaceState() {} },
  { addEventListener() {} }, async () => ({ ok: true, json: async () => [] }),
  { log() {}, error() {} }, () => 0, () => {}, () => 1, () => {}, Date, { timeout: () => ({}) }, sortState);

api.setupTableSorting();

const th = { dataset: { col: 'priority' }, closest: sel => (sel.includes('sortable') ? th : null) };
let prevented = false;

for (const key of ['Enter', ' ']) {
  sortState.col = null;
  prevented = false;
  listeners.keydown({ key, target: th, preventDefault: () => { prevented = true; } });
  if (sortState.col !== 'priority' || !prevented) {
    console.error('Expected keydown ' + key + ' to sort and prevent default');
    process.exit(1);
  }
}

process.stdout.write(JSON.stringify({ ok: true }));
