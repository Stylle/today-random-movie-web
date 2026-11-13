const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');
test('library page size fills complete rows and respects backend limit', async () => {
  const { pageSizeForColumns } = await import('../frontend/src/library-layout.mjs');
  for (const columns of [1, 2, 3, 5, 7, 8, 20]) {
    assert.equal(pageSizeForColumns(columns), columns * 4);
    assert.equal(pageSizeForColumns(columns) % columns, 0);
  }
  assert.equal(pageSizeForColumns(NaN), 4);
  assert.equal(pageSizeForColumns(0), 4);
  assert.equal(pageSizeForColumns(100), 100);
});
test('library uses responsive page size, cancels obsolete requests and cleans observers', () => {
  const view = read('frontend/src/views/LibraryView.vue');
  assert.match(view, /pageSize: String\(pageSize.value\)/);
  assert.doesNotMatch(view, /pageSize: '30'|total > 30/);
  assert.match(view, /new ResizeObserver/);
  assert.match(view, /id !== generation/);
  assert.match(view, /observer\?\.disconnect/);
});
test('management and search entries are not duplicated across the header and home', () => {
  const app = read('frontend/src/App.vue'), home = read('frontend/src/views/HomeView.vue');
  assert.doesNotMatch(app, /href="#\/admin"/);
  assert.match(app, /class="icon-btn manage-entry"/);
  assert.match(app, /!store.user \|\| route === 'admin'/);
  assert.doesNotMatch(home, /搜索影片/);
});
test('poster width, neutral nav selection and equal-height home panels are explicit', () => {
  const css = read('frontend/src/style.css');
  assert.match(css, /\.poster-wrap\{width:100%;max-width:none;max-height:none;aspect-ratio:2\/3/);
  assert.match(css, /\.poster-wrap img\{position:absolute;inset:0;width:100%;height:100%/);
  assert.match(css, /\.app-shell \.topbar nav a.active\{color:var\(--text\)/);
  assert.match(css, /\.home-view \.picker-layout>\.filter-panel\{align-self:stretch/);
});
