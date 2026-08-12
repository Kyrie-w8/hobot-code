const GIB = 1024 ** 3;

export function boardHealth(snapshot) {
  if (!snapshot) return {tone: 'neutral', issues: []};

  const issues = [];
  const maximum = Math.max(-Infinity, ...snapshot.thermalZones.map((zone) => zone.celsius));
  if (Number.isFinite(maximum) && maximum >= 85) {
    issues.push({tone: 'danger', label: `Board temperature is ${maximum.toFixed(0)} C. Pause heavy workloads and check cooling.`});
  } else if (Number.isFinite(maximum) && maximum >= 75) {
    issues.push({tone: 'warning', label: `Board temperature is ${maximum.toFixed(0)} C. Sustained workloads may throttle.`});
  }

  const memoryRatio = ratio(snapshot.memory.availableBytes, snapshot.memory.totalBytes);
  if (snapshot.memory.totalBytes > 0 && (snapshot.memory.availableBytes < 512 * 1024 ** 2 || memoryRatio < 0.05)) {
    issues.push({tone: 'danger', label: 'Memory is critically low. Stop unused tasks before starting a model workload.'});
  } else if (snapshot.memory.totalBytes > 0 && (snapshot.memory.availableBytes < GIB || memoryRatio < 0.1)) {
    issues.push({tone: 'warning', label: 'Available memory is low for model conversion or inference.'});
  }

  const diskRatio = ratio(snapshot.disk.availableBytes, snapshot.disk.totalBytes);
  if (snapshot.disk.totalBytes > 0 && (snapshot.disk.availableBytes < GIB || diskRatio < 0.03)) {
    issues.push({tone: 'danger', label: 'Hobot Code storage is critically low. Free space before continuing.'});
  } else if (snapshot.disk.totalBytes > 0 && (snapshot.disk.availableBytes < 4 * GIB || diskRatio < 0.1)) {
    issues.push({tone: 'warning', label: 'Storage is running low for model artifacts and task history.'});
  }

  if (snapshot.boardId !== 'unknown' && snapshot.bpuDevices.length === 0) {
    issues.push({tone: 'warning', label: 'No BPU device was detected. Board-side AI workloads may not run.'});
  }
  if (!snapshot.rdkUtilities.hrt_model_exec) {
    issues.push({tone: 'warning', label: 'hrt_model_exec is unavailable. BPU model validation is not ready.'});
  }

  const orphaned = snapshot.aiMemory?.ionOrphanedBytes ?? 0;
  if (orphaned > 0) {
    issues.push({tone: 'warning', label: `${formatBytes(orphaned)} of orphaned ION buffers were detected. Check long-running media or accelerator processes.`});
  }

  const cma = snapshot.aiMemory;
  if (cma?.cmaAvailable && cma.cmaTotalBytes > 0 && cma.cmaFreeBytes / cma.cmaTotalBytes < 0.1) {
    issues.push({tone: 'warning', label: 'Contiguous memory is low. Camera or accelerator buffer allocation may fail.'});
  }

  const tone = issues.some((issue) => issue.tone === 'danger')
    ? 'danger'
    : issues.some((issue) => issue.tone === 'warning') ? 'warning' : 'healthy';
  return {tone, issues};
}

export function capacityPair(available, total) {
  if (!total) return '-';
  return `${formatBytes(available)} / ${formatBytes(total)}`;
}

export function durationLabel(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) return '-';
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  if (days) return `${days}d ${hours}h`;
  const minutes = Math.floor((seconds % 3600) / 60);
  return hours ? `${hours}h ${minutes}m` : `${minutes}m`;
}

export function maximumTemperature(snapshot) {
  const value = Math.max(-Infinity, ...snapshot.thermalZones.map((zone) => zone.celsius));
  return Number.isFinite(value) ? `${value.toFixed(1)} C` : '-';
}

export function temperatureTone(snapshot) {
  const value = Math.max(-Infinity, ...snapshot.thermalZones.map((zone) => zone.celsius));
  if (!Number.isFinite(value)) return 'neutral';
  if (value >= 85) return 'danger';
  if (value >= 75) return 'warning';
  return 'healthy';
}

export function bpuCoreLabel(snapshot) {

  const count = snapshot.bpuCores?.length || snapshot.bpuDevices.filter((device) => /\/bpu_core\d+$/.test(device)).length || (snapshot.bpuDevices.some((device) => /\/bpu$/.test(device)) ? 1 : 0);
  return count ? `${count} core${count === 1 ? '' : 's'} ready` : 'Not detected';
}

export function bpuUtilization(snapshot) {
  const cores = snapshot.bpuCores ?? [];
  if (!cores.length) return {available: false, average: 0, peak: 0, peakCore: 0};
  const values = cores.map((core) => clampPercent(core.utilizationPercent));
  const peak = Math.max(...values);
  return {available: true, average: values.reduce((sum, value) => sum + value, 0) / values.length, peak, peakCore: values.indexOf(peak)};
}

export function bpuTemperature(snapshot) {
  const values = snapshot.thermalZones.filter((zone) => /bpu/i.test(zone.name)).map((zone) => zone.celsius);
  const value = Math.max(-Infinity, ...values);
  return Number.isFinite(value) ? `${value.toFixed(1)} C` : 'Unavailable';
}

export function bpuFrequency(snapshot) {
  const cores = snapshot.bpuCores ?? [];
  const current = Math.max(0, ...cores.map((core) => core.currentFrequencyHz ?? 0));
  const maximum = Math.max(0, ...cores.map((core) => core.maximumFrequencyHz ?? 0));
  if (!current) return 'Unavailable';
  return maximum ? `${formatFrequency(current)} / ${formatFrequency(maximum)}` : formatFrequency(current);
}

export function aiMemoryLabel(snapshot) {
  const memory = snapshot.aiMemory;
  if (!memory?.available) return 'Unavailable';
  if (memory.bpuAllocationAvailable) return `${formatBytes(memory.bpuAllocatedBytes ?? 0)} BPU`;
  if (memory.ionAvailable) return `${formatBytes(memory.ionAllocatedBytes ?? 0)} ION`;
  if (memory.cmaAvailable) return `${formatBytes(memory.cmaFreeBytes ?? 0)} CMA free`;
  if (memory.dmaBufAvailable) return `${formatBytes(memory.dmaBufBytes ?? 0)} shared`;
  return 'Available';
}

export function percentLabel(value) {
  return `${Math.round(clampPercent(value))}%`;
}

export function loadLabel(snapshot) {
  const load = snapshot.loadAverage[0];
  return Number.isFinite(load) ? `${load.toFixed(2)} / ${snapshot.cpuCores || '-'}` : '-';
}

function ratio(available, total) {
  return total > 0 ? available / total : 1;
}

export function formatBytes(value) {
  if (!Number.isFinite(value) || value < 0) return '-';
  if (value >= GIB) {
    const gibibytes = value / GIB;
    return `${Number.isInteger(gibibytes) ? gibibytes.toFixed(0) : gibibytes.toFixed(gibibytes >= 10 ? 0 : 1)} GiB`;
  }
  return `${Math.round(value / 1024 ** 2)} MiB`;
}

function formatFrequency(value) {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(value % 1_000_000_000 === 0 ? 0 : 1)} GHz`;
  return `${Math.round(value / 1_000_000)} MHz`;
}

function clampPercent(value) {
  return Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : 0;
}
