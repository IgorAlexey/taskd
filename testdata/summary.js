'use strict';
const fs = require('fs');
const { createElement } = require('./dom.js');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

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

const cases = {
  serverSummary: { summary: 'Fix the parser' },
  serverTruncated: { summary: 'A'.repeat(50) + '\u2026' },
  emptySummaryWithAsset: { summary: '', asset_path: 'models/car.glb' },
  missingSummaryWithAsset: { asset_path: 'models/car.glb' },
  noSummaryNoAsset: {},
  bodyNeverUsed: { body: 'raw body first line\nsecond line' },
  bodyIgnoredWhenSummaryPresent: { summary: 'server summary', body: 'raw body\nsecond line' },
  markup: { summary: '<img src=x onerror=alert(1)> & co' },
};

const out = { summary: {}, rowHTML: {} };
for (const [name, task] of Object.entries(cases)) {
  out.summary[name] = api.rowFields(task).summary;
  out.rowHTML[name] = api.createRow(Object.assign({ id: 't1' }, task), false).innerHTML;
}

process.stdout.write(JSON.stringify(out, null, 2));
