'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import Link from 'next/link';
import Logo from '@/components/shared/Logo';
import UserChip from '@/components/shared/UserChip';
import SignedImage from '@/components/shared/SignedImage';
import { authFetch } from '@/lib/api';

// ── Types ────────────────────────────────────────────────────────────────────

interface LabelJob {
  id: number;
  alarm_id: string;
  camera_id: string;
  site_id: string;
  snapshot_url: string;
  vlm_description: string;
  vlm_threat: string;
  vlm_model: string;
  yolo_detections: unknown[];
  status: string;
  created_at: string;
}

interface LabelingStats {
  pending: number;
  claimed: number;
  labeled: number;
  skipped: number;
  total: number;
  correct: number;
  incorrect: number;
  needs_correction: number;
}

const VERDICT_OPTS = [
  { value: 'correct',          label: 'Correct',           hint: 'VLM description and threat level are accurate',           color: '#22c55e' },
  { value: 'incorrect',        label: 'Incorrect',          hint: 'VLM output is wrong — provide corrections below',          color: '#ef4444' },
  { value: 'needs_correction', label: 'Needs Correction',   hint: 'Partially right — adjust description or threat level',     color: '#f59e0b' },
] as const;

const THREAT_OPTS = ['none', 'low', 'medium', 'high', 'critical'] as const;

const QUICK_TAGS = [
  'false_positive', 'true_positive', 'person', 'vehicle', 'animal',
  'ppe_violation', 'weapon', 'trespassing', 'edge_case', 'poor_image_quality',
];

function authedFetch(url: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers ?? {});
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json');
  }
  return authFetch(url, { ...init, headers });
}

// ── Stats bar ─────────────────────────────────────────────────────────────────

function StatsBar({ stats }: { stats: LabelingStats | null }) {
  if (!stats) return null;
  const pct = stats.total > 0 ? Math.round((stats.labeled / stats.total) * 100) : 0;
  return (
    <div style={{ display: 'flex', gap: 24, padding: '12px 24px', background: 'var(--bg-secondary, #0E1117)', borderBottom: '1px solid rgba(255,255,255,0.06)', flexWrap: 'wrap', alignItems: 'center' }}>
      {[
        { label: 'Pending',  value: stats.pending,  color: '#f59e0b' },
        { label: 'Labeled',  value: stats.labeled,  color: '#22c55e' },
        { label: 'Skipped',  value: stats.skipped,  color: '#4a5268' },
        { label: 'Total',    value: stats.total,    color: '#8891a5' },
      ].map(s => (
        <div key={s.label} style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
          <span style={{ fontSize: 20, fontWeight: 700, color: s.color, fontFamily: "'JetBrains Mono', monospace" }}>{s.value}</span>
          <span style={{ fontSize: 10, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1 }}>{s.label}</span>
        </div>
      ))}
      <div style={{ flex: 1, minWidth: 120 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: 'var(--text-muted)', marginBottom: 3 }}>
          <span>Progress</span><span>{pct}%</span>
        </div>
        <div style={{ height: 4, background: 'rgba(255,255,255,0.06)', borderRadius: 2 }}>
          <div style={{ width: `${pct}%`, height: '100%', background: '#22c55e', borderRadius: 2, transition: 'width 0.3s' }} />
        </div>
      </div>
      {/* Verdict breakdown */}
      <div style={{ display: 'flex', gap: 10, fontSize: 10, color: 'var(--text-muted)' }}>
        <span style={{ color: '#22c55e' }}>{stats.correct} correct</span>
        <span style={{ color: '#ef4444' }}>{stats.incorrect} wrong</span>
        <span style={{ color: '#f59e0b' }}>{stats.needs_correction} partial</span>
      </div>
    </div>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function LabelingPage() {
  const [stats, setStats] = useState<LabelingStats | null>(null);
  const [job, setJob] = useState<LabelJob | null>(null);
  const [queueEmpty, setQueueEmpty] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  // Form state
  const [verdict, setVerdict] = useState<string>('');
  const [corrDesc, setCorrDesc] = useState('');
  const [corrThreat, setCorrThreat] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [notes, setNotes] = useState('');
  const [customTag, setCustomTag] = useState('');

  // Job list panel
  const [showQueue, setShowQueue] = useState(false);
  const [queueJobs, setQueueJobs] = useState<LabelJob[]>([]);
  const [queueStatus, setQueueStatus] = useState('pending');

  const imgRef = useRef<HTMLImageElement>(null);

  const fetchStats = useCallback(async () => {
    const r = await authedFetch('/api/admin/labeling/stats');
    if (r.ok) setStats(await r.json());
  }, []);

  const resetForm = useCallback(() => {
    setVerdict('');
    setCorrDesc('');
    setCorrThreat('');
    setTags([]);
    setNotes('');
    setCustomTag('');
    setMsg(null);
  }, []);

  const claimNext = useCallback(async () => {
    setLoading(true);
    resetForm();
    try {
      const r = await authedFetch('/api/admin/labeling/jobs/next', { method: 'POST' });
      const data = await r.json();
      if (data.queue_empty) {
        setJob(null);
        setQueueEmpty(true);
      } else {
        setJob(data.job);
        setQueueEmpty(false);
      }
    } catch {
      setMsg({ ok: false, text: 'Failed to fetch next job.' });
    }
    setLoading(false);
    await fetchStats();
  }, [resetForm, fetchStats]);

  const submitLabel = useCallback(async (skipIt = false) => {
    if (!job) return;
    if (!skipIt && !verdict) {
      setMsg({ ok: false, text: 'Select a verdict before saving.' });
      return;
    }
    setSaving(true);
    try {
      const body = skipIt
        ? { verdict: 'skipped' }
        : { verdict, corrected_description: corrDesc, corrected_threat: corrThreat, tags, notes };
      const r = await authedFetch(`/api/admin/labeling/jobs/${job.id}/label`, {
        method: 'POST',
        body: JSON.stringify(body),
      });
      if (!r.ok) throw new Error(await r.text());
      await claimNext();
    } catch (e: unknown) {
      setMsg({ ok: false, text: e instanceof Error ? e.message : 'Save failed.' });
    }
    setSaving(false);
  }, [job, verdict, corrDesc, corrThreat, tags, notes, claimNext]);

  const loadQueue = useCallback(async (status: string) => {
    const r = await authedFetch(`/api/admin/labeling/jobs?status=${status}&limit=100`);
    if (r.ok) setQueueJobs(await r.json());
  }, []);

  // Keyboard shortcuts: 1=correct, 2=incorrect, 3=needs_correction, Enter=save, S=skip
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      if (e.key === '1') setVerdict('correct');
      if (e.key === '2') setVerdict('incorrect');
      if (e.key === '3') setVerdict('needs_correction');
      if (e.key === 'Enter' && !saving) submitLabel();
      if (e.key === 's' || e.key === 'S') submitLabel(true);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [submitLabel, saving]);

  // Load stats and first job on mount
  useEffect(() => {
    fetchStats();
    claimNext();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const toggleTag = (t: string) => setTags(prev => prev.includes(t) ? prev.filter(x => x !== t) : [...prev, t]);

  const addCustomTag = () => {
    const t = customTag.trim().toLowerCase().replace(/\s+/g, '_');
    if (t && !tags.includes(t)) setTags(prev => [...prev, t]);
    setCustomTag('');
  };

  const snapshotSrc = job?.snapshot_url
    ? (job.snapshot_url.startsWith('http') ? job.snapshot_url : job.snapshot_url)
    : null;

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-primary, #090D14)', display: 'flex', flexDirection: 'column', fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
      {/* Top bar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 24px', background: 'var(--bg-secondary, #0E1117)', borderBottom: '1px solid rgba(255,255,255,0.06)', flexShrink: 0 }}>
        <Logo height={18} />
        <span style={{ fontSize: 12, color: 'var(--text-muted)', fontWeight: 400 }}>/ Admin</span>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>/</span>
        <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--accent-orange)' }}>ML Labeling</span>
        <div style={{ flex: 1 }} />
        <Link href="/admin" style={{ fontSize: 11, color: 'var(--text-muted)', textDecoration: 'none' }}>← Admin</Link>
        <button
          onClick={() => { setShowQueue(v => !v); if (!showQueue) loadQueue(queueStatus); }}
          style={{ padding: '5px 12px', fontSize: 11, borderRadius: 4, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: 'var(--text-secondary)', cursor: 'pointer', fontFamily: 'inherit' }}
        >
          Queue
        </button>
        <a
          href="/api/admin/labeling/export?verdict=all"
          style={{ padding: '5px 12px', fontSize: 11, borderRadius: 4, background: 'rgba(34,197,94,0.08)', border: '1px solid rgba(34,197,94,0.2)', color: '#22c55e', textDecoration: 'none' }}
        >
          Export JSONL
        </a>
        <UserChip />
      </div>

      <StatsBar stats={stats} />

      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>

        {/* ── Queue Panel ── */}
        {showQueue && (
          <div style={{ width: 300, background: 'var(--bg-secondary, #0E1117)', borderRight: '1px solid rgba(255,255,255,0.06)', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
            <div style={{ padding: '12px 14px', borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Queue</div>
              <div style={{ display: 'flex', gap: 4 }}>
                {['pending', 'labeled', 'skipped'].map(s => (
                  <button key={s} onClick={() => { setQueueStatus(s); loadQueue(s); }}
                    style={{ flex: 1, padding: '4px 6px', fontSize: 10, borderRadius: 3, border: 'none', cursor: 'pointer', fontFamily: 'inherit',
                      background: queueStatus === s ? 'rgba(232,115,42,0.15)' : 'rgba(255,255,255,0.04)',
                      color: queueStatus === s ? 'var(--accent-orange)' : 'var(--text-muted)',
                    }}>
                    {s}
                  </button>
                ))}
              </div>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', scrollbarWidth: 'thin' }}>
              {queueJobs.map(j => (
                <div key={j.id}
                  onClick={() => { setJob(j); resetForm(); setShowQueue(false); }}
                  style={{ padding: '10px 14px', borderBottom: '1px solid rgba(255,255,255,0.04)', cursor: 'pointer', transition: 'background 0.1s' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                >
                  <div style={{ fontSize: 10, color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", marginBottom: 2 }}>#{j.id} · {j.alarm_id.slice(-8)}</div>
                  <div style={{ fontSize: 11, color: '#e4e8f0', lineHeight: 1.3 }}>{j.vlm_description.slice(0, 80)}{j.vlm_description.length > 80 ? '…' : ''}</div>
                  <div style={{ fontSize: 9, color: j.vlm_threat === 'high' || j.vlm_threat === 'critical' ? '#ef4444' : '#f59e0b', marginTop: 3, textTransform: 'uppercase', fontWeight: 600 }}>{j.vlm_threat}</div>
                </div>
              ))}
              {queueJobs.length === 0 && <div style={{ padding: 20, fontSize: 12, color: 'var(--text-muted)', textAlign: 'center' }}>No {queueStatus} jobs</div>}
            </div>
          </div>
        )}

        {/* ── Main annotation area ── */}
        <div style={{ flex: 1, display: 'flex', gap: 0, overflow: 'hidden' }}>

          {/* Frame viewer */}
          <div style={{ flex: '0 0 55%', display: 'flex', flexDirection: 'column', background: '#050810', borderRight: '1px solid rgba(255,255,255,0.06)' }}>
            {loading ? (
              <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 13 }}>Loading frame…</div>
            ) : queueEmpty ? (
              <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 12 }}>
                <div style={{ fontSize: 32 }}>✓</div>
                <div style={{ fontSize: 15, fontWeight: 600, color: '#22c55e' }}>Queue empty</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>All pending jobs have been labeled.</div>
                <button onClick={fetchStats} style={{ marginTop: 8, padding: '7px 16px', fontSize: 12, borderRadius: 5, background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.25)', color: '#22c55e', cursor: 'pointer', fontFamily: 'inherit' }}>Refresh</button>
              </div>
            ) : !job ? (
              <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <button onClick={claimNext} style={{ padding: '10px 24px', fontSize: 13, fontWeight: 600, borderRadius: 6, background: 'rgba(232,115,42,0.1)', border: '1px solid rgba(232,115,42,0.3)', color: 'var(--accent-orange)', cursor: 'pointer', fontFamily: 'inherit' }}>
                  Start Labeling
                </button>
              </div>
            ) : (
              <>
                {/* Image */}
                <div style={{ flex: 1, position: 'relative', overflow: 'hidden', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  {snapshotSrc ? (
                    // imgRef intentionally unused: pre-P1-A-03 attempts
                    // captured an HTMLImageElement here for hypothetical
                    // bbox-overlay computations; none ever wired up.
                    // SignedImage doesn't forward refs (it has its own
                    // resolve-then-render lifecycle), so dropping the
                    // attribute is the right move.
                    <SignedImage
                      src={snapshotSrc}
                      alt="Alarm frame"
                      style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain' }}
                      onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
                    />
                  ) : (
                    <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>No snapshot available</div>
                  )}
                  {/* Threat badge overlay */}
                  <div style={{
                    position: 'absolute', top: 10, left: 10,
                    padding: '3px 10px', borderRadius: 4, fontSize: 10, fontWeight: 700, letterSpacing: 0.5, textTransform: 'uppercase',
                    background: job.vlm_threat === 'high' || job.vlm_threat === 'critical' ? 'rgba(239,68,68,0.85)' : 'rgba(245,158,11,0.85)',
                    color: '#fff',
                  }}>
                    {job.vlm_threat}
                  </div>
                  {/* Job ID */}
                  <div style={{ position: 'absolute', top: 10, right: 10, padding: '2px 8px', borderRadius: 3, fontSize: 9, background: 'rgba(0,0,0,0.7)', color: '#8891a5', fontFamily: "'JetBrains Mono', monospace" }}>
                    #{job.id}
                  </div>
                </div>

                {/* VLM description read-only */}
                <div style={{ padding: '12px 16px', borderTop: '1px solid rgba(255,255,255,0.06)', background: 'rgba(255,255,255,0.02)' }}>
                  <div style={{ fontSize: 9, color: 'var(--text-muted)', letterSpacing: 1, textTransform: 'uppercase', marginBottom: 5 }}>VLM Output · {job.vlm_model}</div>
                  <div style={{ fontSize: 12, color: '#c8d0e0', lineHeight: 1.5 }}>{job.vlm_description}</div>
                  <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 5, fontFamily: "'JetBrains Mono', monospace" }}>
                    cam: {job.camera_id.slice(0, 8)} · site: {job.site_id.slice(0, 8)} · alarm: {job.alarm_id.slice(-12)}
                  </div>
                </div>
              </>
            )}
          </div>

          {/* Label form */}
          <div style={{ flex: 1, overflowY: 'auto', scrollbarWidth: 'thin', padding: '20px 20px 40px' }}>
            {job && !loading && (
              <>
                {/* Keyboard hint */}
                <div style={{ marginBottom: 14, padding: '7px 12px', borderRadius: 5, background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.05)', fontSize: 10, color: 'var(--text-muted)', display: 'flex', gap: 16, flexWrap: 'wrap' }}>
                  <span><kbd style={{ padding: '1px 5px', borderRadius: 3, background: 'rgba(255,255,255,0.07)', fontSize: 9 }}>1</kbd> Correct</span>
                  <span><kbd style={{ padding: '1px 5px', borderRadius: 3, background: 'rgba(255,255,255,0.07)', fontSize: 9 }}>2</kbd> Incorrect</span>
                  <span><kbd style={{ padding: '1px 5px', borderRadius: 3, background: 'rgba(255,255,255,0.07)', fontSize: 9 }}>3</kbd> Partial</span>
                  <span><kbd style={{ padding: '1px 5px', borderRadius: 3, background: 'rgba(255,255,255,0.07)', fontSize: 9 }}>Enter</kbd> Save</span>
                  <span><kbd style={{ padding: '1px 5px', borderRadius: 3, background: 'rgba(255,255,255,0.07)', fontSize: 9 }}>S</kbd> Skip</span>
                </div>

                {/* Verdict */}
                <div style={{ marginBottom: 16 }}>
                  <div style={{ fontSize: 10, fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: 0.8, marginBottom: 6 }}>Verdict</div>
                  <div style={{ display: 'flex', gap: 8 }}>
                    {VERDICT_OPTS.map(v => (
                      <button key={v.value} onClick={() => setVerdict(v.value)}
                        style={{
                          flex: 1, padding: '10px 8px', borderRadius: 6, cursor: 'pointer', fontFamily: 'inherit', textAlign: 'left',
                          background: verdict === v.value ? `${v.color}15` : 'rgba(255,255,255,0.02)',
                          border: `1px solid ${verdict === v.value ? v.color + '50' : 'rgba(255,255,255,0.07)'}`,
                          transition: 'all 0.15s',
                        }}
                      >
                        <div style={{ fontSize: 11, fontWeight: 600, color: verdict === v.value ? v.color : 'var(--text-secondary)', marginBottom: 3 }}>{v.label}</div>
                        <div style={{ fontSize: 9, color: 'var(--text-muted)', lineHeight: 1.3 }}>{v.hint}</div>
                      </button>
                    ))}
                  </div>
                </div>

                {/* Corrections — shown when verdict is not correct */}
                {verdict && verdict !== 'correct' && (
                  <div style={{ marginBottom: 16, padding: 14, borderRadius: 6, background: 'rgba(239,68,68,0.04)', border: '1px solid rgba(239,68,68,0.15)' }}>
                    <div style={{ fontSize: 10, fontWeight: 600, color: '#ef9b8b', textTransform: 'uppercase', letterSpacing: 0.8, marginBottom: 10 }}>Corrections</div>
                    <div style={{ marginBottom: 10 }}>
                      <label style={{ fontSize: 10, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Corrected Description</label>
                      <textarea
                        value={corrDesc}
                        onChange={e => setCorrDesc(e.target.value)}
                        placeholder={job.vlm_description}
                        rows={3}
                        style={{ width: '100%', padding: '8px 10px', fontSize: 12, borderRadius: 5, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.1)', color: '#e4e8f0', fontFamily: 'inherit', resize: 'vertical', boxSizing: 'border-box' }}
                      />
                    </div>
                    <div>
                      <label style={{ fontSize: 10, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Correct Threat Level</label>
                      <div style={{ display: 'flex', gap: 6 }}>
                        {THREAT_OPTS.map(t => {
                          const active = corrThreat === t;
                          const col = t === 'critical' || t === 'high' ? '#ef4444' : t === 'medium' ? '#f59e0b' : t === 'low' ? '#22c55e' : '#4a5268';
                          return (
                            <button key={t} onClick={() => setCorrThreat(active ? '' : t)}
                              style={{ flex: 1, padding: '5px 4px', fontSize: 10, fontWeight: 600, borderRadius: 4, border: `1px solid ${active ? col + '60' : 'rgba(255,255,255,0.08)'}`, background: active ? `${col}15` : 'transparent', color: active ? col : 'var(--text-muted)', cursor: 'pointer', fontFamily: 'inherit', textTransform: 'uppercase', letterSpacing: 0.3 }}>
                              {t}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  </div>
                )}

                {/* Tags */}
                <div style={{ marginBottom: 16 }}>
                  <div style={{ fontSize: 10, fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: 0.8, marginBottom: 6 }}>Tags</div>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 8 }}>
                    {QUICK_TAGS.map(t => (
                      <button key={t} onClick={() => toggleTag(t)}
                        style={{ padding: '3px 10px', fontSize: 10, borderRadius: 10, cursor: 'pointer', fontFamily: 'inherit',
                          background: tags.includes(t) ? 'rgba(232,115,42,0.15)' : 'rgba(255,255,255,0.04)',
                          border: `1px solid ${tags.includes(t) ? 'rgba(232,115,42,0.4)' : 'rgba(255,255,255,0.07)'}`,
                          color: tags.includes(t) ? 'var(--accent-orange)' : 'var(--text-muted)',
                        }}>
                        {t}
                      </button>
                    ))}
                  </div>
                  <div style={{ display: 'flex', gap: 6 }}>
                    <input
                      value={customTag}
                      onChange={e => setCustomTag(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && (e.preventDefault(), addCustomTag())}
                      placeholder="custom tag…"
                      style={{ flex: 1, padding: '5px 10px', fontSize: 11, borderRadius: 4, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#e4e8f0', fontFamily: 'inherit' }}
                    />
                    <button onClick={addCustomTag} style={{ padding: '5px 12px', fontSize: 11, borderRadius: 4, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: 'var(--text-muted)', cursor: 'pointer', fontFamily: 'inherit' }}>Add</button>
                  </div>
                </div>

                {/* Notes */}
                <div style={{ marginBottom: 20 }}>
                  <label style={{ fontSize: 10, fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: 0.8, display: 'block', marginBottom: 4 }}>Notes (optional)</label>
                  <textarea
                    value={notes}
                    onChange={e => setNotes(e.target.value)}
                    placeholder="Any context that would help the model…"
                    rows={2}
                    style={{ width: '100%', padding: '8px 10px', fontSize: 12, borderRadius: 5, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#e4e8f0', fontFamily: 'inherit', resize: 'vertical', boxSizing: 'border-box' }}
                  />
                </div>

                {/* Feedback */}
                {msg && (
                  <div style={{ marginBottom: 12, fontSize: 11, color: msg.ok ? '#22c55e' : '#ef4444', padding: '6px 10px', borderRadius: 4, background: msg.ok ? 'rgba(34,197,94,0.06)' : 'rgba(239,68,68,0.06)' }}>
                    {msg.text}
                  </div>
                )}

                {/* Action buttons */}
                <div style={{ display: 'flex', gap: 8 }}>
                  <button
                    onClick={() => submitLabel(true)}
                    disabled={saving}
                    style={{ padding: '9px 16px', fontSize: 12, borderRadius: 5, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: 'var(--text-muted)', cursor: 'pointer', fontFamily: 'inherit' }}
                  >
                    Skip
                  </button>
                  <button
                    onClick={() => submitLabel(false)}
                    disabled={saving || !verdict}
                    style={{
                      flex: 1, padding: '9px 16px', fontSize: 12, fontWeight: 600, borderRadius: 5, cursor: verdict ? 'pointer' : 'not-allowed',
                      background: verdict ? 'rgba(232,115,42,0.15)' : 'rgba(255,255,255,0.03)',
                      border: `1px solid ${verdict ? 'rgba(232,115,42,0.4)' : 'rgba(255,255,255,0.07)'}`,
                      color: verdict ? 'var(--accent-orange)' : 'var(--text-muted)',
                      fontFamily: 'inherit', transition: 'all 0.15s',
                    }}
                  >
                    {saving ? 'Saving…' : 'Save & Next'}
                  </button>
                </div>
              </>
            )}

            {!job && !loading && !queueEmpty && (
              <div style={{ textAlign: 'center', paddingTop: 60 }}>
                <button onClick={claimNext} style={{ padding: '10px 24px', fontSize: 13, fontWeight: 600, borderRadius: 6, background: 'rgba(232,115,42,0.1)', border: '1px solid rgba(232,115,42,0.3)', color: 'var(--accent-orange)', cursor: 'pointer', fontFamily: 'inherit' }}>
                  Start Labeling
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
