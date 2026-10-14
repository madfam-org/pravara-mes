#!/usr/bin/env node
/**
 * NPM Security Audit gate for pravara-ui.
 *
 * Fails on any advisory at or above the threshold (moderate) unless
 * `npm-audit-allowlist.json` waives it with a reason and a review_by date that
 * has not passed. Every vulnerable package in `npm audit --json` traces back to
 * advisory objects in some package's `via`, so the gate judges advisories: a
 * transitive package is cleared exactly when every advisory behind it is.
 *
 *   npm audit --json > npm-audit-results.json || true
 *   node scripts/npm-audit-gate.mjs npm-audit-results.json npm-audit-allowlist.json
 */
import { readFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

const RANK = { info: 0, low: 1, moderate: 2, high: 3, critical: 4 }

export function advisoriesOf(report) {
  const found = new Map()
  for (const [name, vuln] of Object.entries(report.vulnerabilities ?? {})) {
    for (const via of vuln.via ?? []) {
      if (typeof via !== 'object' || via === null) continue // a transitive pointer, not an advisory
      const id = String(via.url ?? '').split('/').pop() || `npm-${via.source}`
      const entry = found.get(id) ?? { id, severity: via.severity, title: via.title, packages: new Set() }
      entry.packages.add(via.name ?? name)
      found.set(id, entry)
    }
  }
  return [...found.values()]
}

export function evaluate(report, allowlist, { threshold = 'moderate', today = new Date().toISOString().slice(0, 10) } = {}) {
  if (!report || typeof report !== 'object' || report.error || !report.metadata) {
    // A registry outage or a broken install must not read as "no findings".
    return { failures: [{ id: 'npm-audit', severity: 'critical', packages: new Set(), why: 'npm audit produced no report' }], waived: [] }
  }
  const allowed = new Map((allowlist.advisories ?? []).map((entry) => [entry.id, entry]))
  const failures = []
  const waived = []
  for (const advisory of advisoriesOf(report)) {
    if ((RANK[advisory.severity] ?? RANK.critical) < RANK[threshold]) continue
    const entry = allowed.get(advisory.id)
    if (!entry) failures.push({ ...advisory, why: 'not allow-listed' })
    else if (!entry.reason || !entry.review_by) failures.push({ ...advisory, why: 'allow-list entry needs a reason and a review_by date' })
    else if (entry.review_by < today) failures.push({ ...advisory, why: `allow-list entry expired on ${entry.review_by}; re-review it` })
    else waived.push({ ...advisory, review_by: entry.review_by })
  }
  return { failures, waived }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [reportPath, allowlistPath] = process.argv.slice(2)
  let report
  try { report = JSON.parse(readFileSync(reportPath, 'utf8')) } catch { report = null }
  const allowlist = JSON.parse(readFileSync(allowlistPath, 'utf8'))
  const { failures, waived } = evaluate(report, allowlist)
  for (const w of waived) console.log(`waived  ${w.id} (${w.severity}) ${[...w.packages].join(', ')}; review by ${w.review_by}`)
  for (const f of failures) console.log(`FAIL    ${f.id} (${f.severity}) ${[...f.packages].join(', ')}: ${f.why}`)
  console.log(`npm audit gate: ${failures.length} failing, ${waived.length} waived`)
  process.exit(failures.length ? 1 : 0)
}
