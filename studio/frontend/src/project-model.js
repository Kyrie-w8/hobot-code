export function groupTasksByProject(tasks) {
  const groups = new Map();
  for (const task of tasks) groups.set(task.cwd, [...(groups.get(task.cwd) ?? []), task]);
  return [...groups.entries()]
    .map(([path, projectTasks]) => ({path, name: path === '/root' ? 'General' : basename(path), tasks: projectTasks}))
    .sort((left, right) => left.name.localeCompare(right.name));
}

export function arrangeTasks(tasks) {
  const byId = new Map(tasks.map((task) => [task.id, task]));
  const newestEditByParent = new Map();
  for (const task of tasks) {
    if (task.branchKind !== 'edit' || !task.parentTaskId || !byId.has(task.parentTaskId)) continue;
    const existing = newestEditByParent.get(task.parentTaskId);
    if (!existing || editTime(task) > editTime(existing)) newestEditByParent.set(task.parentTaskId, task);
  }
  const latestEdit = (task) => {
    let current = task;
    const seen = new Set([current.id]);
    while (newestEditByParent.has(current.id)) {
      const next = newestEditByParent.get(current.id);
      if (seen.has(next.id)) break;
      seen.add(next.id);
      current = next;
    }
    return current;
  };
  const editBase = (task) => {
    let current = task;
    const seen = new Set([current.id]);
    while (current.branchKind === 'edit' && current.parentTaskId && byId.has(current.parentTaskId) && !seen.has(current.parentTaskId)) {
      seen.add(current.parentTaskId);
      current = byId.get(current.parentTaskId);
    }
    return current;
  };
  const rootID = (task) => {
    let current = task;
    const seen = new Set([current.id]);
    while (current.parentTaskId && byId.has(current.parentTaskId) && !seen.has(current.parentTaskId)) {
      seen.add(current.parentTaskId);
      current = byId.get(current.parentTaskId);
    }
    return current.id;
  };
  const byParent = new Map();
  for (const base of tasks.filter((task) => task.branchKind !== 'edit')) {
    const task = latestEdit(base);
    let parent = '';
    let branchKind = '';
    if (base.branchKind === 'side' && base.parentTaskId && byId.has(base.parentTaskId)) {
      const root = byId.get(rootID(byId.get(base.parentTaskId)));
      parent = latestEdit(editBase(root)).id;
      branchKind = 'side';
    }
    byParent.set(parent, [...(byParent.get(parent) ?? []), {task, branchKind}]);
  }
  for (const task of tasks.filter((item) => item.branchKind === 'edit')) {
    if (editBase(task).id !== task.id) continue;
    byParent.set('', [...(byParent.get('') ?? []), {task, branchKind: ''}]);
  }
  const result = [];
  const seen = new Set();
  const visit = (parent, depth) => {
    for (const entry of (byParent.get(parent) ?? []).sort((left, right) => taskTime(right.task) - taskTime(left.task))) {
      const {task, branchKind} = entry;
      if (seen.has(task.id)) continue;
      seen.add(task.id);
      result.push({task, depth, branchKind});
      visit(task.id, depth + 1);
    }
  };
  visit('', 0);
  for (const entries of byParent.values()) {
    for (const {task, branchKind} of entries) {
      if (!seen.has(task.id)) result.push({task, depth: 0, branchKind});
    }
  }
  return result;
}

function taskTime(task) {
  return new Date(task.updatedAt ?? task.createdAt ?? 0).getTime();
}

function editTime(task) {
  return new Date(task.createdAt ?? task.updatedAt ?? 0).getTime();
}

function basename(path) {
  return path.split('/').filter(Boolean).at(-1) ?? path;
}
