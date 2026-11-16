import assert from 'node:assert/strict'
import test from 'node:test'
import { executionDuration, queueDuration, formatDuration, recordedDuration } from './timing.js'

test('live retry totals exclude idle time and stop after completion', () => {
  const startedAt = '2026-11-16T10:02:03Z'
  const queuedAt = '2026-11-16T10:02:00Z'
  const now = Date.parse('2026-11-16T10:02:05Z')
  const job = { status: 'running', durationMs: 4000, queueDurationMs: 5000, startedAt, queuedAt }
  assert.equal(executionDuration(job, now), 6000)
  assert.equal(queueDuration(job, now), 5000)
  assert.equal(executionDuration({ ...job, status: 'success', durationMs: 7000 }, now + 60000), 7000)
  assert.equal(queueDuration({ ...job, status: 'queued', startedAt: null }, now), 10000)
  assert.equal(executionDuration(job, Date.parse(startedAt) - 1000), 4000)
})

test('legacy and partial records remain distinguishable from zero duration', () => {
  const legacy = { status: 'success', createdAt: '2026-11-16T10:00:00Z', durationMs: 0 }
  assert.equal(executionDuration(legacy), null)
  assert.equal(queueDuration(legacy), null)
  assert.equal(recordedDuration(legacy, null), '未记录')
  assert.equal(recordedDuration({ status: 'blocked' }, null), '—')
  const skipped = { status: 'skipped', finishedAt: '2026-11-16T10:00:00Z' }
  assert.equal(executionDuration(skipped), 0)
  assert.equal(recordedDuration({ timingIncomplete: true }, 1234), '≥ 1.2 秒')
  assert.equal(recordedDuration({ timingEstimated: true }, 1234), '≈ 1.2 秒')
})

test('duration formatting supports milliseconds, minutes and hours', () => {
  assert.equal(formatDuration(12), '0.01 秒')
  assert.equal(formatDuration(59999), '60.0 秒')
  assert.equal(formatDuration(61000), '1 分 1 秒')
  assert.equal(formatDuration(3600000), '1 时 0 分')
  assert.equal(formatDuration(null), '未记录')
})
