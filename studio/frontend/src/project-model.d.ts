import type {Task} from './types';

export type ProjectGroup = {path: string; name: string; tasks: Task[]};
export type ArrangedTask = {task: Task; depth: number; branchKind: '' | 'side'};

export function groupTasksByProject(tasks: Task[]): ProjectGroup[];
export function arrangeTasks(tasks: Task[]): ArrangedTask[];
