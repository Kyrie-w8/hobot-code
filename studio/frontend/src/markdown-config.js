import rehypeKatex from 'rehype-katex';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';

// A single tilde is commonly used for ranges and approximations in model output.
// Keep strikethrough support, but require the standard double-tilde delimiter.
export const markdownRemarkPlugins = [remarkMath, [remarkGfm, {singleTilde: false}]];
export const markdownRehypePlugins = [rehypeKatex];
