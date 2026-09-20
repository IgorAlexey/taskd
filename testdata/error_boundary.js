'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const banner = { hidden: true };
const bannerText = { textContent: '' };
const els = {
  'error-banner': banner,
  'error-banner-text': bannerText,
};

const windowListeners = {};
const windowObj = {
  addEventListener(type, fn) {
    (windowListeners[type] = windowListeners[type] || []).push(fn);
  },
  dispatch(type, event) {
    (windowListeners[type] || []).forEach(fn => fn(event));
  },
};

const document = {
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nloadAll = async () => {};'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, windowObj,
  () => {}, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

windowObj.dispatch('error', { message: 'test uncaught error' });
const errorCaptured = { hidden: banner.hidden, text: bannerText.textContent };

windowObj.dispatch('unhandledrejection', { reason: new Error('test unhandled rejection') });
const rejectionCaptured = { hidden: banner.hidden, text: bannerText.textContent };

process.stdout.write(JSON.stringify({
  errorCaptured,
  rejectionCaptured,
}));
