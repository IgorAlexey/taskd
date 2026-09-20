'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const detailsPane = {
  id: 'task-details',
  focus() { document.activeElement = this; },
};
const detailsContent = { innerHTML: '', scrollTop: 0 };

const listeners = {};
function createRow(id) {
  const row = {
    dataset: { taskId: id },
    setAttribute() {},
    removeAttribute() {},
    focus() { document.activeElement = this; },
    closest(sel) {
      if (sel.includes('data-task-id')) return this;
      return null;
    },
  };
  return row;
}

const tr1 = createRow(1);
const tr2 = createRow(2);
const tr3 = createRow(3);
tr1.nextElementSibling = tr2;
tr2.previousElementSibling = tr1;
tr2.nextElementSibling = tr3;
tr3.previousElementSibling = tr2;
const rows = [tr1, tr2, tr3];

const tbody = {
  addEventListener(event, handler) {
    listeners[event] = handler;
  },
  querySelectorAll() { return rows; },
};

const els = {
  'task-details': detailsPane,
  'task-details-content': detailsContent,
  'task-table-body': tbody,
};

const document = {
  activeElement: null,
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

const fetchStub = async (url) => {
  if (url.includes('/tasks/1')) {
    return {
      ok: true,
      status: 200,
      headers: { get: () => null },
      text: async () => JSON.stringify({ id: 1, project: 'p', priority: 1 }),
    };
  }
  return { ok: true, status: 200, headers: { get: () => null }, text: async () => '{}' };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { setupRowActivation, selectTask };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  api.setupRowActivation();

  document.activeElement = null;
  const clickEvent = { target: tr1 };
  listeners.click(clickEvent);
  const clickFocus = (document.activeElement === tr1);

  document.activeElement = tr1;
  let ctrlPrevented = false;
  listeners.keydown({
    key: 'Enter',
    ctrlKey: true,
    preventDefault() { ctrlPrevented = true; },
    target: tr1,
  });
  const ctrlIgnored = (!ctrlPrevented && document.activeElement === tr1);

  let inputPrevented = false;
  const inputEl = {
    closest(sel) {
      if (sel.includes('input')) return this;
      if (sel.includes('data-task-id')) return tr1;
      return null;
    },
  };
  listeners.keydown({
    key: ' ',
    preventDefault() { inputPrevented = true; },
    target: inputEl,
  });
  const inputIgnored = (!inputPrevented && document.activeElement === tr1);

  document.activeElement = tr1;
  let enterPrevented = false;
  listeners.keydown({
    key: 'Enter',
    preventDefault() { enterPrevented = true; },
    target: tr1,
  });
  const enterFocus = (document.activeElement === detailsPane);

  document.activeElement = tr1;
  listeners.keydown({
    key: 'ArrowDown',
    preventDefault() {},
    target: tr1,
  });
  const arrowFocus = (document.activeElement === tr2);

  document.activeElement = tr1;
  listeners.keydown({
    key: 'j',
    preventDefault() {},
    target: tr1,
  });
  const jFocus = (document.activeElement === tr2);

  document.activeElement = tr2;
  listeners.keydown({
    key: 'k',
    preventDefault() {},
    target: tr2,
  });
  const kFocus = (document.activeElement === tr1);

  document.activeElement = tr2;
  listeners.keydown({
    key: 'ArrowUp',
    preventDefault() {},
    target: tr2,
  });
  const arrowUpFocus = (document.activeElement === tr1);

  document.activeElement = tr1;
  listeners.keydown({
    key: 'End',
    preventDefault() {},
    target: tr1,
  });
  const endFocus = (document.activeElement === tr3);

  document.activeElement = tr3;
  listeners.keydown({
    key: 'Home',
    preventDefault() {},
    target: tr3,
  });
  const homeFocus = (document.activeElement === tr1);

  document.activeElement = tr1;
  listeners.keydown({
    key: 'PageDown',
    preventDefault() {},
    target: tr1,
  });
  const pageDownFocus = (document.activeElement === tr3);

  document.activeElement = tr3;
  listeners.keydown({
    key: 'PageUp',
    preventDefault() {},
    target: tr3,
  });
  const pageUpFocus = (document.activeElement === tr1);

  process.stdout.write(JSON.stringify({
    clickFocus,
    ctrlIgnored,
    inputIgnored,
    enterFocus,
    enterPrevented,
    arrowFocus,
    jFocus,
    kFocus,
    arrowUpFocus,
    homeFocus,
    endFocus,
    pageDownFocus,
    pageUpFocus,
  }));
})();
