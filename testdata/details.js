'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

let innerHTMLSetCount = 0;
let innerHTMLVal = '';

const pane = {
  get innerHTML() { return innerHTMLVal; },
  set innerHTML(v) {
    innerHTMLSetCount++;
    innerHTMLVal = v;
  },
  scrollTop: 0,
};

const els = {
  'task-details-content': pane,
  'error-banner': { style: {} },
};

const document = {
  getElementById: id => els[id] || null,
  createElement: () => ({ style: {}, dataset: {}, setAttribute() {}, innerHTML: '' }),
};

let taskData = { id: 't1', project: 'p1', status: 'pending', body: 'hello' };

const fetchStub = async url => {
  if (url === '/tasks/t1') {
    const raw = JSON.stringify(taskData);
    return {
      ok: true,
      status: 200,
      text: async () => raw,
      json: async () => JSON.parse(raw),
      headers: { get: () => 'application/json' },
    };
  }
  return { ok: false, status: 404, text: async () => 'not found' };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn { selectTask, loadTaskDetails, resetTaskDetails };'
)(document, { pathname: '/ui', search: '', hash: '' }, { replaceState() {}, pushState() {} }, { addEventListener() {} }, fetchStub, { log() {}, error() {} }, () => 0, () => {});

(async () => {
  api.selectTask('t1');
  await new Promise(r => setTimeout(r, 10));
  const afterFirst = innerHTMLSetCount;

  await api.loadTaskDetails('t1');
  const afterSecond = innerHTMLSetCount;
  if (afterSecond !== afterFirst) {
    console.error('expected innerHTML untouched on identical refresh');
    process.exit(1);
  }

  taskData.status = 'leased';
  taskData.worker = 'w1';
  await api.loadTaskDetails('t1');
  if (innerHTMLSetCount !== afterSecond + 1) {
    console.error('expected innerHTML updated on changed content');
    process.exit(2);
  }

  api.resetTaskDetails();
  const afterReset = innerHTMLSetCount;

  api.selectTask('t1');
  await new Promise(r => setTimeout(r, 10));
  if (innerHTMLSetCount !== afterReset + 1) {
    console.error('expected innerHTML updated on re-selection');
    process.exit(3);
  }

  process.exit(0);
})();
