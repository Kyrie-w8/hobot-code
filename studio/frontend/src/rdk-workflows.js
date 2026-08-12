const workflows = [
  {
    id: 'diagnose',
    title: 'Diagnose this board',
    prompt: 'Inspect this RDK board and workspace. Check the exact board and RDK OS version, BPU devices and runtime tools, temperature, memory, disk, recent system errors, and project state. Report blockers first and do not change the system without approval.',
  },
  {
    id: 'deploy-model',
    title: 'Deploy a model',
    prompt: 'Inspect the model files in this workspace and prepare a deployment plan for this exact RDK board. Verify framework, input shapes, preprocessing, toolchain compatibility, quantization requirements, runtime commands, and an accuracy and latency acceptance test before changing files.',
  },
  {
    id: 'camera',
    title: 'Debug camera pipeline',
    prompt: 'Diagnose the camera and multimedia pipeline on this RDK board. Enumerate the actual devices and media topology, verify sensor and RDK OS compatibility, inspect relevant logs, and propose the smallest safe test that proves capture through the expected VIN, ISP, and output path.',
  },
  {
    id: 'tros',
    title: 'Check TROS workspace',
    prompt: 'Inspect this TROS or ROS 2 workspace for this exact RDK OS. Verify distribution, packages, environment, launch files, topics, device dependencies, and build state. Identify the first reproducible failure and propose a safe fix with a validation command.',
  },
  {
    id: 'benchmark',
    title: 'Validate BPU performance',
    prompt: 'Build a reproducible BPU validation for this workspace and board. Confirm the deployed artifact matches the board, record input and preprocessing, run a small correctness check before latency measurement, and report warmup, iterations, latency distribution, throughput, temperature, and memory with artifact paths.',
  },
];

export function rdkWorkflows(boardId) {
  const preferred = boardId === 'unknown'
    ? ['diagnose', 'deploy-model', 'camera']
    : ['diagnose', 'deploy-model', 'benchmark', 'camera', 'tros'];
  return preferred.map((id) => workflows.find((workflow) => workflow.id === id));
}
