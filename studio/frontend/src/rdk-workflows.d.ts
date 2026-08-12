export type RDKWorkflow = {id: string; title: string; prompt: string};
export function rdkWorkflows(boardId?: string): RDKWorkflow[];
