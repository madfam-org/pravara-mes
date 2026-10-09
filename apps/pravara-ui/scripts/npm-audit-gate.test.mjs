import { test } from 'node:test'
import assert from 'node:assert/strict'
import { advisoriesOf, evaluate } from './npm-audit-gate.mjs'

const advisory = (name, id, severity) => ({ name, severity, title: `${name} issue`, url: `https://github.com/advisories/${id}`, source: 1 })
const report = {
  metadata: { vulnerabilities: { high: 2, moderate: 1, low: 1 } },
  vulnerabilities: {
    braces: { name: 'braces', severity: 'high', via: [advisory('braces', 'GHSA-aaaa', 'high')] },
    micromatch: { name: 'micromatch', severity: 'high', via: ['braces'] },
    parser: { name: 'parser', severity: 'moderate', via: [advisory('parser', 'GHSA-bbbb', 'moderate')] },
    tiny: { name: 'tiny', severity: 'low', via: [advisory('tiny', 'GHSA-cccc', 'low')] },
  },
}
const allow = (id, review_by = '2099-01-01', reason = 'no fix') => ({ id, reason, review_by })

test('collects advisories, not transitive pointers', () => {
  assert.deepEqual(advisoriesOf(report).map((a) => a.id).sort(), ['GHSA-aaaa', 'GHSA-bbbb', 'GHSA-cccc'])
})

test('fails on an advisory at or above moderate that is not allow-listed, and ignores low', () => {
  const { failures, waived } = evaluate(report, { advisories: [allow('GHSA-aaaa')] }, { today: '2026-10-09' })
  assert.deepEqual(failures.map((f) => f.id), ['GHSA-bbbb'])
  assert.deepEqual(waived.map((w) => w.id), ['GHSA-aaaa'])
})

test('passes when every advisory at or above moderate is waived', () => {
  const { failures } = evaluate(report, { advisories: [allow('GHSA-aaaa'), allow('GHSA-bbbb')] }, { today: '2026-10-09' })
  assert.equal(failures.length, 0)
})

test('an expired or reasonless waiver fails again', () => {
  const expired = evaluate(report, { advisories: [allow('GHSA-aaaa', '2026-10-01'), allow('GHSA-bbbb')] }, { today: '2026-10-09' })
  assert.match(expired.failures[0].why, /expired on 2026-10-01/)
  const reasonless = evaluate(report, { advisories: [allow('GHSA-aaaa', '2099-01-01', ''), allow('GHSA-bbbb')] }, { today: '2026-10-09' })
  assert.match(reasonless.failures[0].why, /needs a reason/)
})

test('a missing or failed audit report fails the gate', () => {
  assert.equal(evaluate(null, { advisories: [] }).failures.length, 1)
  assert.equal(evaluate({ error: { code: 'ENOTFOUND' } }, { advisories: [] }).failures.length, 1)
})
