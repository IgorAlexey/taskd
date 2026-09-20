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
      options: [],
      hidden: false,
      disabled: false,
      attributes: {},
      listeners: {},
      setAttribute(k, v) { this.attributes[k] = String(v); },
      getAttribute(k) { return this.attributes[k] || null; },
      addEventListener(evt, fn) { this.listeners[evt] = fn; },
      focus() {},
      dispatchEvent(evt) {
        evt.target = this;
        if (this.listeners[evt.type]) {
          this.listeners[evt.type](evt);
        }
      },
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

const sampleTask = {
  id: 101,
  project: 'proj-alpha',
  status: 'pending',
  priority: 15,
  claim_count: 0,
  body: 'Fix the widget layout',
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\ncurrentTask = ' + JSON.stringify(sampleTask) + ';\nlet saveCalls = 0;\nsaveTaskEdit = function() { saveCalls++; };\nreturn { renderTaskDetails, getSaveCalls: () => saveCalls };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  () => {}, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

api.renderTaskDetails(sampleTask, false);
const editBtn = getOrCreate('edit-task-btn');
if (editBtn.onclick) {
  editBtn.onclick();
}

const fields = ['edit-task-project', 'edit-task-priority', 'edit-task-body'];
const results = {};

for (const fieldId of fields) {
  const el = getOrCreate(fieldId);
  results[fieldId] = { ctrlS: false, cmdS: false, shiftIgnored: false, altIgnored: false };

  const startCallsCtrl = api.getSaveCalls();
  const evtCtrl = {
    type: 'keydown',
    key: 's',
    ctrlKey: true,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  };
  el.dispatchEvent(evtCtrl);
  if (evtCtrl.defaultPrevented && api.getSaveCalls() > startCallsCtrl) {
    results[fieldId].ctrlS = true;
  }

  const startCallsCmd = api.getSaveCalls();
  const evtCmd = {
    type: 'keydown',
    key: 's',
    ctrlKey: false,
    metaKey: true,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  };
  el.dispatchEvent(evtCmd);
  if (evtCmd.defaultPrevented && api.getSaveCalls() > startCallsCmd) {
    results[fieldId].cmdS = true;
  }

  const startCallsShift = api.getSaveCalls();
  const evtShift = {
    type: 'keydown',
    key: 's',
    ctrlKey: true,
    metaKey: false,
    shiftKey: true,
    altKey: false,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  };
  el.dispatchEvent(evtShift);
  if (!evtShift.defaultPrevented && api.getSaveCalls() === startCallsShift) {
    results[fieldId].shiftIgnored = true;
  }

  const startCallsAlt = api.getSaveCalls();
  const evtAlt = {
    type: 'keydown',
    key: 's',
    ctrlKey: true,
    metaKey: false,
    shiftKey: false,
    altKey: true,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  };
  el.dispatchEvent(evtAlt);
  if (!evtAlt.defaultPrevented && api.getSaveCalls() === startCallsAlt) {
    results[fieldId].altIgnored = true;
  }
}

process.stdout.write(JSON.stringify(results));
