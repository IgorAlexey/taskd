'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const attrs = { 'aria-busy': 'false' };
const submitBtn = {
  disabled: false,
  setAttribute(k, v) { attrs[k] = String(v); },
  getAttribute(k) { return attrs[k] || null; },
  removeAttribute(k) { delete attrs[k]; },
};

const detailsPane = {
  textContent: '',
  innerHTML: '',
  replaceChildren() {},
  setAttribute() {},
  removeAttribute() {},
  focus() {},
};

const tbody = {
  querySelectorAll: () => [],
  addEventListener() {},
};

const els = {
  'submit-task-btn': submitBtn,
  'form-project': { value: 'default', focus() {} },
  'form-body': { value: 'test body', focus() {} },
  'form-priority': { value: '' },
  'form-error-summary': { hidden: true, setAttribute() {}, removeAttribute() {} },
  'form-success-summary': { hidden: true, setAttribute() {}, removeAttribute() {} },
  'form-error-summary-list': { replaceChildren() {} },
  'form-success-summary-body': { textContent: '' },
  'task-details-content': detailsPane,
  'task-table-body': tbody,
  'connection-status': { hidden: true, textContent: '' },
};

const document = {
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

let detailsFetchCount = 0;
const createdId = 1;

const fetchStub = async (url, opts) => {
  if (url === '/tasks' && opts && opts.method === 'POST') {
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => ({ id: createdId }),
    };
  }
  if (url === '/tasks/' + encodeURIComponent(createdId)) {
    detailsFetchCount++;
    const taskObj = {
      id: createdId,
      project: 'default',
      body: 'test body',
      status: 'pending',
      priority: 1,
      claim_count: 0,
    };
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      text: async () => JSON.stringify(taskObj),
      json: async () => taskObj,
    };
  }
  return {
    ok: true,
    status: 200,
    headers: { get: () => 'application/json' },
    text: async () => '[]',
    json: async () => ([]),
  };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { submitTask, getSelectedTaskId: () => selectedTaskId };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  await api.submitTask();
  const selectedTaskId = api.getSelectedTaskId();

  process.stdout.write(JSON.stringify({
    selectedTaskId,
    detailsFetchCount,
  }));
})();
