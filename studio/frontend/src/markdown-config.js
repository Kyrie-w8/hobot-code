import remarkGfm from 'remark-gfm';

// A single tilde is commonly used for ranges and approximations in model output.
// Keep strikethrough support, but require the standard double-tilde delimiter.
export const markdownRemarkPlugins = [[remarkGfm, {singleTilde: false}]];
