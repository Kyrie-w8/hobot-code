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

function ratio(available, total) {
  return total > 0 ? available / total : 1;
}

function formatBytes(value) {
  if (!Number.isFinite(value) || value < 0) return '-';
  if (value >= GIB) {
    const gibibytes = value / GIB;
    return `${Number.isInteger(gibibytes) ? gibibytes.toFixed(0) : gibibytes.toFixed(gibibytes >= 10 ? 0 : 1)} GiB`;
  }
  return `${Math.round(value / 1024 ** 2)} MiB`;
}
