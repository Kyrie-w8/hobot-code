export const maximumRenderedDiffLines = 4000;

export function workspaceChangeSummary(changes) {
  if (changes?.available === false) return {title: 'Git review unavailable', detail: 'Install Git from the board OS packages, then refresh this view.'};
  if (!changes?.repository) return {title: 'No Git repository', detail: 'This task workspace is not inside a Git repository.'};
  const count = changes.files?.length ?? 0;
  if (count === 0 && !changes.filesTruncated) return {title: 'Working tree clean', detail: 'No tracked or untracked changes were detected in this task workspace.'};
  return {
    title: `${count}${changes.filesTruncated ? '+' : ''} changed file${count === 1 && !changes.filesTruncated ? '' : 's'}`,
    detail: 'Current workspace snapshot. Changes may come from this Agent, another task, or manual edits.',
  };
}

export function workspaceDiffLines(patch, maximum = maximumRenderedDiffLines) {
  if (!patch) return {lines: [], truncated: false};
  const source = patch.split('\n');
  const truncated = source.length > maximum;
  return {
    lines: source.slice(0, maximum).map((text, index) => ({
      key: `${index}:${text.slice(0, 32)}`,
      text,
      kind: text.startsWith('+++') || text.startsWith('---') || text.startsWith('diff --git') || text.startsWith('index ')
        ? 'meta'
        : text.startsWith('@@') ? 'hunk' : text.startsWith('+') ? 'added' : text.startsWith('-') ? 'deleted' : 'context',
    })),
    truncated,
  };
}

export function workspaceChangeLabel(file) {
  if (file.conflict) return 'Conflict';
  if (file.untracked) return 'Untracked';
  const locations = [file.staged ? 'Staged' : '', file.unstaged ? 'Unstaged' : ''].filter(Boolean).join(' + ');
  return locations || file.kind || 'Changed';
}

export function workspaceDeliverySummary(delivery) {
  if (!delivery) return null;
  if (delivery.alreadyApplied) {
    return {tone: 'success', title: 'Applied to project', detail: 'The isolated changes are staged in the original project.'};
  }
  if (delivery.ready) {
    return {tone: 'ready', title: 'Ready to apply', detail: 'Applying stops idle Agents using either workspace and stages the changes in the original project. It never commits or pushes.'};
  }
  return {tone: 'blocked', title: 'Not ready to apply', detail: delivery.reason || 'The isolated changes cannot be applied yet.'};
}
