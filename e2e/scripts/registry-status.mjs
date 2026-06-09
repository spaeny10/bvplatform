#!/usr/bin/env node
// Maps the latest Playwright JSON report (results/results.json) onto the
// feature-registry IDs in feature-map.json and writes
// results/feature-status.json:
//   [{ feature, status: pass|fail|partial|skipped, passed, failed,
//      skipped, lastRun, baseURL }]
//
// "partial" = no failures, but some tests were skipped or carry a
// soft-skip/parked/no-alarms annotation (a flag-parked page that only got
// its 404 verified is not a full pass for the underlying feature).

import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';
import dotenv from 'dotenv';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
dotenv.config({ path: path.join(root, '.env') });

const resultsPath = path.join(root, 'results', 'results.json');
const mapPath = path.join(root, 'feature-map.json');
const outPath = path.join(root, 'results', 'feature-status.json');

if (!fs.existsSync(resultsPath)) {
    console.error(`No ${resultsPath} — run the tests first (npm run smoke / smoke:all).`);
    process.exit(1);
}

const report = JSON.parse(fs.readFileSync(resultsPath, 'utf8'));
const featureMap = JSON.parse(fs.readFileSync(mapPath, 'utf8'));
delete featureMap._comment;

const PARTIAL_ANNOTATIONS = new Set(['parked', 'soft-skip', 'no-alarms', 'flag-unknown', 'decode-unavailable']);

// ── walk the suite tree, tally per spec-file basename ──
const perFile = new Map(); // basename -> {passed, failed, skipped, annotated}

function tally(file) {
    const base = path.basename(file);
    if (!perFile.has(base)) perFile.set(base, { passed: 0, failed: 0, skipped: 0, annotated: 0 });
    return perFile.get(base);
}

function walkSuite(suite, fileFromParent) {
    const file = suite.file || fileFromParent;
    for (const spec of suite.specs ?? []) {
        const t = tally(spec.file || file);
        for (const testEntry of spec.tests ?? []) {
            const annotations = testEntry.annotations ?? [];
            const hasPartialAnno = annotations.some(a => PARTIAL_ANNOTATIONS.has(a.type));
            switch (testEntry.status) {
                case 'skipped':
                    t.skipped += 1;
                    if (hasPartialAnno) t.annotated += 1;
                    break;
                case 'unexpected':
                    t.failed += 1;
                    break;
                case 'flaky': // passed on retry
                case 'expected':
                    t.passed += 1;
                    if (hasPartialAnno) t.annotated += 1;
                    break;
                default:
                    break;
            }
        }
    }
    for (const child of suite.suites ?? []) walkSuite(child, file);
}

for (const suite of report.suites ?? []) walkSuite(suite, undefined);

// ── fold spec-file tallies into features ──
const lastRun = report.stats?.startTime ?? new Date().toISOString();
const baseURL = process.env.IRONSIGHT_BASE_URL || 'http://127.0.0.1:13000';

const featureTotals = new Map(); // feature -> {passed, failed, skipped, annotated, specs}
for (const [specFile, features] of Object.entries(featureMap)) {
    const t = perFile.get(specFile);
    if (!t) continue; // spec not in this run (grep-filtered)
    for (const feature of features) {
        if (!featureTotals.has(feature)) {
            featureTotals.set(feature, { passed: 0, failed: 0, skipped: 0, annotated: 0, specs: [] });
        }
        const f = featureTotals.get(feature);
        f.passed += t.passed;
        f.failed += t.failed;
        f.skipped += t.skipped;
        f.annotated += t.annotated;
        f.specs.push(specFile);
    }
}

function statusOf(f) {
    if (f.failed > 0) return 'fail';
    if (f.passed === 0) return 'skipped';
    if (f.skipped > 0 || f.annotated > 0) return 'partial';
    return 'pass';
}

const rows = [...featureTotals.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([feature, f]) => ({
        feature,
        status: statusOf(f),
        passed: f.passed,
        failed: f.failed,
        skipped: f.skipped,
        lastRun,
        baseURL,
    }));

fs.mkdirSync(path.dirname(outPath), { recursive: true });
fs.writeFileSync(outPath, JSON.stringify(rows, null, 2) + '\n');

// Console summary
const width = Math.max(...rows.map(r => r.feature.length), 7);
console.log(`feature-status (${rows.length} features, run ${lastRun}, base ${baseURL})`);
for (const r of rows) {
    console.log(`  ${r.feature.padEnd(width)}  ${r.status.padEnd(7)}  pass=${r.passed} fail=${r.failed} skip=${r.skipped}`);
}
console.log(`\nwrote ${outPath}`);
