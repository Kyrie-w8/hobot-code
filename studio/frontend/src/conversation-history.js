export const eventPageSize = 200;

export function mergeEventHistory(current, incoming) {
  const bySequence = new Map();
  for (const event of [...current, ...incoming]) {
    if (!Number.isSafeInteger(event.sequence) || event.sequence <= 0) throw new Error('History page contained an invalid event sequence.');
    const existing = bySequence.get(event.sequence);
    if (existing && JSON.stringify(existing) !== JSON.stringify(event)) throw new Error('History page contained conflicting event records.');
    bySequence.set(event.sequence, event);
  }
  return [...bySequence.values()].sort((left, right) => left.sequence - right.sequence);
}

export function navigationEventWindow(events) {
  const window = mergeEventHistory([], events ?? []);
  for (let index = 1; index < window.length; index += 1) {
    if (window[index].sequence !== window[index - 1].sequence + 1) throw new Error('History page contained a sequence gap.');
  }
  return window;
}

export function eventPageContinuesAfter(after, events) {
  return (events ?? []).every((event, index) => event.sequence === after + index + 1);
}

export function eventPageHasLater(page) {
  const lastSequence = page.events?.at(-1)?.sequence ?? 0;
  const retainedThrough = page.retainedThrough ?? page.latestSequence ?? lastSequence;
  return Boolean(page.hasMore || (lastSequence > 0 && retainedThrough > lastSequence));
}

export function userMessagesFromEvents(events) {
  return events.flatMap((event) => {
    const data = event.normalized?.type === 'user.message' ? event.normalized.data ?? {} : null;
    const text = data ? String(data.text ?? '') : '';
    if (!text || data?.source === 'schedule') return [];
    return [{kind: 'user', key: `user-${event.sequence}`, sequence: event.sequence, time: event.time, text, attachments: [], source: 'user', scheduleId: ''}];
  });
}

export function mergeMessageIndex(current, incoming) {
  const messages = new Map(current.map((message) => [message.sequence, message]));
  for (const message of incoming) messages.set(message.sequence, message);
  return [...messages.values()].sort((left, right) => left.sequence - right.sequence);
}

export function navigatorGroups(messages, maxMarkers = 120) {
  if (messages.length < 6) return [];
  const markerCount = Math.min(Math.max(1, maxMarkers), messages.length);
  const groupSize = Math.ceil(messages.length / markerCount);
  return Array.from({length: markerCount}, (_, index) => messages.slice(index * groupSize, (index + 1) * groupSize)).filter((group) => group.length > 0);
}

export function historyPageBefore(all, before = 0, limit = 200) {
  const cap = Math.max(1, Math.min(200, Math.floor(limit) || 200));
  const eligible = before === 0 ? all : all.filter((event) => event.sequence < before);
  const events = eligible.slice(-cap);
  return {
    events,
    nextAfter: events.at(-1)?.sequence,
    hasMore: false,
    nextBefore: events[0]?.sequence,
    hasEarlier: events.length > 0 && events[0].sequence > 1,
    retainedFrom: all[0]?.sequence,
    retainedThrough: all.at(-1)?.sequence,
    latestSequence: all.at(-1)?.sequence,
    historyTruncated: false,
    cursorExpired: false,
  };
}
