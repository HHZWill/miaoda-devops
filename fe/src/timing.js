function elapsed(value, now) {
  const started = Date.parse(value)
  // 缺失时间或时钟回拨不产生负耗时。
  return Number.isFinite(started) ? Math.max(0, now - started) : 0
}

export function executionDuration(item, now = Date.now()) {
  // 旧记录没有计时起点，不能用创建时间推算。
  if (!item.startedAt && !item.queuedAt && !item.durationMs
    && !(item.status === 'skipped' && item.finishedAt)) return null
  return (item.durationMs || 0) + (item.status === 'running' ? elapsed(item.startedAt, now) : 0)
}

export function queueDuration(job, now = Date.now()) {
  // 没有排队记录的旧任务保持未知。
  if (!job.queuedAt) return null
  return (job.queueDurationMs || 0) + (job.status === 'queued' ? elapsed(job.queuedAt, now) : 0)
}

export function formatDuration(milliseconds) {
  if (milliseconds == null) return '未记录'
  const ms = Math.max(0, milliseconds)
  if (ms < 1000) return (ms / 1000).toFixed(2) + ' 秒'
  if (ms < 60000) return (ms / 1000).toFixed(1) + ' 秒'
  const seconds = Math.floor(ms / 1000)
  if (seconds < 3600) return Math.floor(seconds / 60) + ' 分 ' + seconds % 60 + ' 秒'
  return Math.floor(seconds / 3600) + ' 时 ' + Math.floor(seconds % 3600 / 60) + ' 分'
}

export function recordedDuration(item, milliseconds) {
  if (milliseconds == null) return ['pending', 'blocked'].includes(item.status) ? '—' : '未记录'
  const prefix = item.timingIncomplete ? '≥ ' : item.timingEstimated ? '≈ ' : ''
  return prefix + formatDuration(milliseconds)
}
