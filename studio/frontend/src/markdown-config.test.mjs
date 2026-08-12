import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import ReactMarkdown from 'react-markdown';

import {markdownRemarkPlugins} from './markdown-config.js';

function render(markdown) {
  return renderToStaticMarkup(React.createElement(ReactMarkdown, {
    remarkPlugins: markdownRemarkPlugins,
    children: markdown,
  }));
}

test('single tildes around CLI values remain visible text', () => {
  const output = render('`--cache_len` (2564096) ~ `--chunk_size` (1282048) ~ `--calib_text_path`');
  assert.doesNotMatch(output, /<del>/);
  assert.match(output, /~ <code>--chunk_size<\/code> \(1282048\) ~/);
});

test('standard double-tilde strikethrough remains supported', () => {
  assert.match(render('keep ~~remove~~ keep'), /<del>remove<\/del>/);
});
