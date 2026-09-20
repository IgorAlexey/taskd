'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const els = {};
function getOrCreate(id) {
  if (!els[id]) {
    els[id] = {
      id,
      value: '',
      textContent: '',
      innerHTML: '',
      options: [],
      hidden: false,
      attributes: {},
      listeners: {},
      setAttribute(k, v) { this.attributes[k] = String(v); },
      getAttribute(k) { return this.attributes[k] || null; },
      addEventListener(evt, fn) { this.listeners[evt] = fn; },
    };
  }
  return els[id];
}

const document = {
  getElementById: id => getOrCreate(id),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { renderTaskDetails, completeTask, releaseTask };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  () => {}, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

function checkButton(html, id) {
  const m = html.match(new RegExp('<button[^>]*id="' + id + '"[^>]*>'));
  if (!m) return null;
  const tag = m[0];
  return {
    disabled: tag.includes('disabled'),
    ariaDisabled: tag.includes('aria-disabled="true"'),
    state: (tag.match(/data-state="([^"]+)"/) || [])[1] || '',
    title: (tag.match(/title="([^"]+)"/) || [])[1] || '',
  };
}

api.renderTaskDetails({ id: 't-1', status: 'leased', worker: '' }, false);
const htmlWithout = getOrCreate('task-details-content').innerHTML;

api.renderTaskDetails({ id: 't-2', status: 'leased', worker: 'worker-1' }, false);
const htmlWith = getOrCreate('task-details-content').innerHTML;

api.completeTask('t-1', '');
const completeError = getOrCreate('error-banner-text').textContent;

api.releaseTask('t-1', '');
const releaseError = getOrCreate('error-banner-text').textContent;

process.stdout.write(JSON.stringify({
  withoutWorker: {
    complete: checkButton(htmlWithout, 'complete-task-btn'),
    release: checkButton(htmlWithout, 'release-task-btn'),
  },
  withWorker: {
    complete: checkButton(htmlWith, 'complete-task-btn'),
    release: checkButton(htmlWith, 'release-task-btn'),
  },
  completeError,
  releaseError,
}));
