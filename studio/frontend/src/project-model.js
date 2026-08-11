export function groupTasksByProject(tasks) {
  const groups = new Map();
  for (const task of tasks) groups.set(task.cwd, [...(groups.get(task.cwd) ?? []), task]);
  return [...groups.entries()]
    .map(([path, projectTasks]) => ({path, name: path === '/root' ? 'General' : basename(path), tasks: projectTasks}))
    .sort((left, right) => left.name.localeCompare(right.name));
}

export function arrangeTasks(tasks) {
  const byId = new Map(tasks.map((task) => [task.id, task]));
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
  for (const task of tasks) {
    let parent = task.parentTaskId && byId.has(task.parentTaskId) ? task.parentTaskId : '';
    if (task.branchKind === 'side' && parent) parent = rootID(byId.get(parent));
    byParent.set(parent, [...(byParent.get(parent) ?? []), task]);
  }
  const result = [];
  const seen = new Set();
  const visit = (parent, depth) => {
    for (const task of (byParent.get(parent) ?? []).sort((left, right) => new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime())) {
      if (seen.has(task.id)) continue;
      seen.add(task.id);
      result.push({task, depth});
      visit(task.id, depth + 1);
    }
  };
  visit('', 0);
  for (const task of tasks) {
    if (!seen.has(task.id)) result.push({task, depth: 0});
  }
  return result;
}

function basename(path) {
  return path.split('/').filter(Boolean).at(-1) ?? path;
}
