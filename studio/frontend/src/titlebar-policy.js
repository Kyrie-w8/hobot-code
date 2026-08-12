export function shouldToggleMaximise(event) {
  if (event.button !== 0 || event.detail !== 2) return false;
  const target = event.target;
  if (!(target instanceof Element)) return false;
  return !target.closest('button, input, textarea, select, a, summary, [data-titlebar-no-drag]');
}
