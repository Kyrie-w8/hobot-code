import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import ReactMarkdown from 'react-markdown';

import {markdownRehypePlugins, markdownRemarkPlugins} from './markdown-config.js';

function render(markdown) {
  return renderToStaticMarkup(React.createElement(ReactMarkdown, {
    remarkPlugins: markdownRemarkPlugins,
    rehypePlugins: markdownRehypePlugins,
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

test('block math is rendered instead of displaying raw LaTeX', () => {
  const output = render('$$\n\\text{PPL} = \\exp\\left(-\\frac{1}{N}\\sum_{i=1}^{N} \\ln P(w_i \\mid w_{<i})\\right)\n$$');
  assert.match(output, /class="katex-display"/);
  assert.match(output, /class="katex"/);
  assert.match(output, /<math/);
  assert.match(output, /<mfrac>/);
  assert.doesNotMatch(output, /^\$\$/);
});

test('inline math is rendered inside surrounding prose', () => {
  const output = render('Loss is $L = \\frac{1}{N}$ for this run.');
  assert.match(output, /Loss is <span class="math math-inline"><span class="katex">/);
  assert.match(output, /<mfrac>/);
  assert.match(output, /for this run/);
});
