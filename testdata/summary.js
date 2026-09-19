'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

function createElement(tag) {
  let html = '';
  const el = {
    tagName: tag.toUpperCase(),
    value: '',
    textContent: '',
    style: {},
    dataset: {},
    children: [],
    options: [],
    appendChild() {},
    insertBefore() {},
    removeChild() {},
    remove() {},
    addEventListener() {},
    setAttribute() {},
    getAttribute() { return null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    focus() {},
  };
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; },
  });
  return el;
}

const document = {
  getElementById: () => null,
  createElement: tag => createElement(tag),
};
const location = { pathname: '/ui', search: '', hash: '' };
const history = { pushState() {}, replaceState() {} };
const window = { addEventListener() {} };

const fetchStub = async () => ({
  ok: true,
  status: 200,
  text: async () => '[]',
  json: async () => [],
  headers: { get: () => 'application/json' },
});

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn { rowFields, createRow };'
)(document, location, history, window, fetchStub, console, () => 0, () => {});

const long = 'A'.repeat(80);
const cases = {
  multiline: { body: 'Fix the parser\n\nWhy: it drops newlines\nDone when: ok' },
  crlf: { body: 'Windows title\r\nsecond line' },
  leadingBlank: { body: '\n\n  Indented title\nrest' },
  longFirstLine: { body: long + '\nsecond line' },
  blankOnly: { body: '\n \n', asset_path: 'models/car.glb' },
  emptyBody: { body: '', asset_path: 'models/car.glb' },
  noBodyNoAsset: {},
  singleLine: { body: 'just one line' },
  emoji: { body: 'x' + '🚀'.repeat(60) + '\nsecond line' },
  markup: { body: '<img src=x onerror=alert(1)> & co\nsecond line' },
};

const out = { summary: {}, rowHTML: {} };
for (const [name, task] of Object.entries(cases)) {
  out.summary[name] = api.rowFields(task).summary;
  out.rowHTML[name] = api.createRow(Object.assign({ id: 't1' }, task), false).innerHTML;
}

process.stdout.write(JSON.stringify(out, null, 2));
