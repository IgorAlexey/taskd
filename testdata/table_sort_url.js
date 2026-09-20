'use strict';
const { setupHarness } = require('./table_sort');

(async () => {
  const { api, headers, location, requestedURLs, replacedStates } = setupHarness(
    process.argv[2],
    '?sort=priority&order=desc'
  );

  api.setupTableSorting();
  api.applyURLState();

  const initialAriaSort = headers.priority.getAttribute('aria-sort');
  if (initialAriaSort !== 'descending') {
    throw new Error(`expected priority aria-sort="descending", got "${initialAriaSort}"`);
  }

  await api.loadTasks();
  const initialLoadedURL = requestedURLs[requestedURLs.length - 1];
  if (!initialLoadedURL.includes('sort=priority') || !initialLoadedURL.includes('order=desc')) {
    throw new Error(`expected loadTasks with sort=priority&order=desc, got "${initialLoadedURL}"`);
  }

  await api.sort('priority');
  const clickedSearch1 = location.search;
  const clickedAriaSort1 = headers.priority.getAttribute('aria-sort');
  if (!clickedSearch1.includes('sort=priority') || !clickedSearch1.includes('order=asc')) {
    throw new Error(`expected location.search to have sort=priority&order=asc, got "${clickedSearch1}"`);
  }
  if (clickedAriaSort1 !== 'ascending') {
    throw new Error(`expected priority aria-sort="ascending", got "${clickedAriaSort1}"`);
  }

  await api.sort('claim_count');
  const clickedSearch2 = location.search;
  const clickedAriaSort2 = headers.claim_count.getAttribute('aria-sort');
  const oldPrioritySort = headers.priority.getAttribute('aria-sort');
  if (!clickedSearch2.includes('sort=claim_count') || !clickedSearch2.includes('order=asc')) {
    throw new Error(`expected location.search to have sort=claim_count&order=asc, got "${clickedSearch2}"`);
  }
  if (clickedAriaSort2 !== 'ascending') {
    throw new Error(`expected claim_count aria-sort="ascending", got "${clickedAriaSort2}"`);
  }
  if (oldPrioritySort !== null) {
    throw new Error(`expected priority aria-sort to be cleared, got "${oldPrioritySort}"`);
  }

  process.stdout.write(JSON.stringify({
    initialAriaSort,
    initialLoadedURL,
    clickedSearch1,
    clickedAriaSort1,
    clickedSearch2,
    clickedAriaSort2,
    replaceCount: replacedStates.length,
  }));
})();
