'use client';

import { useCallback, useState, useEffect } from 'react';
import type { SearchResult, SearchFilters, SavedSearch } from '@/types/ironsight';
import { formatRelativeTime, formatConfidence } from '@/lib/format';
import SeverityPill from '@/components/shared/SeverityPill';
import { useSearchStore } from '@/stores/search-store';
import { useSearchMutation } from '@/hooks/useSearch';
import { getSavedSearches, createSavedSearch, deleteSavedSearch } from '@/lib/ironsight-api';
import { useSites } from '@/hooks/useSites';
import Logo from '@/components/shared/Logo';
import Link from 'next/link';

/* IRONSight Steel & Fire tokens */
const T = {
  bg: '#0A0C10', bg2: '#0E1117', bg3: '#151921',
  surface: '#1A1F2A', border: 'rgba(255,255,255,0.07)', border2: 'rgba(255,255,255,0.12)',
  text: '#E4E8F0', text2: '#8891A5',
  accent: '#E8732A', accentGlow: 'rgba(232,115,42,0.18)', accentBorder: 'rgba(232,115,42,0.35)',
  red: '#EF4444', amber: '#E89B2A', green: '#22C55E', purple: '#A855F7',
};

export default function SearchPage() {
  // ── Zustand store ──
  const query = useSearchStore((s) => s.query);
  const setQuery = useSearchStore((s) => s.setQuery);
  const results = useSearchStore((s) => s.results);
  const selectedResultId = useSearchStore((s) => s.selectedResultId);
  const selectResult = useSearchStore((s) => s.selectResult);
  const hasSearched = useSearchStore((s) => s.hasSearched);
  const filters = useSearchStore((s) => s.filters);
  const updateFilters = useSearchStore((s) => s.updateFilters);

  // ── React Query mutation ──
  const searchMutation = useSearchMutation();
  const loading = searchMutation.isPending;

  // Real sites feeding the Site filter dropdown. Replaces the hardcoded
  // mock TX-*** values that never matched actual data in cameras.site_id.
  const { data: sitesData = [] } = useSites();

  // ── Saved searches ──
  const [savedSearches, setSavedSearches] = useState<SavedSearch[]>([]);
  const [showSaved, setShowSaved] = useState(false);
  // Removed showCreateIncident: the search page is an investigation tool
  // for SOC operators. Real incidents are auto-created by the alarm pipeline
  // (and visible to customers under /portal/incidents). Manual incident
  // filing lives in the customer portal, not here.

  useEffect(() => { getSavedSearches().then(setSavedSearches); }, []);

  const handleSaveSearch = async () => {
    if (!query.trim()) return;
    const result = await createSavedSearch({ name: query.trim().slice(0, 50), query: query.trim(), filters, created_by: 'Operator', shared: false });
    setSavedSearches(prev => [result, ...prev]);
  };

  const handleLoadSaved = (s: SavedSearch) => {
    setQuery(s.query);
    updateFilters(s.filters);
    setShowSaved(false);
  };

  const handleDeleteSaved = async (id: string) => {
    await deleteSavedSearch(id);
    setSavedSearches(prev => prev.filter(s => s.id !== id));
  };

  const selectedIdx = results.findIndex(r => r.frame_id === selectedResultId);
  const selected = selectedIdx >= 0 ? results[selectedIdx] : null;

  // Early-indexer artifact: a handful of rows have the VLM's raw JSON blob
  // stored in `description` instead of just the parsed sentence. Clean it
  // on read so the UI doesn't show users "{ \"description\": \"A white van…".
  const cleanCaption = (s: string): string => {
    if (!s) return s;
    const trimmed = s.trim();
    if (!trimmed.startsWith('{')) return s;
    const m = trimmed.match(/"description"\s*:\s*"((?:[^"\\]|\\.)*)"/);
    if (m && m[1]) return m[1].replace(/\\"/g, '"');
    return s;
  };

  // Violation type toggles using store filters
  const violationTypes: Record<string, boolean> = {
    no_hard_hat: (filters.violation_types || []).includes('no_hard_hat'),
    no_harness: (filters.violation_types || []).includes('no_harness'),
    zone_breach: (filters.violation_types || []).includes('zone_breach'),
    no_hi_vis: (filters.violation_types || []).includes('no_hi_vis'),
    vehicle_hazard: (filters.violation_types || []).includes('vehicle_hazard'),
  };

  const toggleViolation = (type: string) => {
    const current = filters.violation_types || [];
    const next = current.includes(type)
      ? current.filter(t => t !== type)
      : [...current, type];
    updateFilters({ violation_types: next });
  };

  const handleSearch = useCallback(() => {
    // Allow the search to fire with no query text when at least one
    // structured filter (violation type) is set — that's a legitimate
    // "show me every PPE violation" query even with an empty search box.
    const activeViolations = Object.entries(violationTypes)
      .filter(([_, v]) => v)
      .map(([k]) => k);
    if (!query.trim() && activeViolations.length === 0) return;

    // Pass every filter the sidebar exposes so the backend can apply them.
    // Previous version dropped date_range / site_ids / confidence_min /
    // time_of_day, which made those controls cosmetic.
    searchMutation.mutate({
      query: query.trim(),
      violation_types: activeViolations.length > 0 ? activeViolations : undefined,
      site_ids: filters.site_ids && filters.site_ids.length > 0 ? filters.site_ids : undefined,
      date_range: filters.date_range?.start || filters.date_range?.end ? filters.date_range : undefined,
      confidence_min: filters.confidence_min,
      time_of_day: filters.time_of_day?.start || filters.time_of_day?.end ? filters.time_of_day : undefined,
      model: filters.model || 'hybrid',
    });
  }, [query, violationTypes, filters.model, filters.site_ids, filters.date_range, filters.confidence_min, filters.time_of_day, searchMutation]);

  // Scene backgrounds for thumbnail placeholders
  const sceneBgs = [
    'linear-gradient(135deg, #141a0c, #1a200e 50%, #0a0f08)',
    'linear-gradient(160deg, #0a1520, #151f10 40%, #0d1818)',
    'linear-gradient(200deg, #0f1510, #1a1208 60%, #0c1015)',
    'linear-gradient(140deg, #08121a, #12100a 50%, #0a1510)',
    'linear-gradient(180deg, #0d1010, #181510 50%, #0a0f15)',
    'linear-gradient(170deg, #10120a, #0c1510 60%, #151008)',
  ];

  return (
    <div style={{
      background: T.bg, color: T.text, fontFamily: "var(--font-family, 'Inter', sans-serif)",
      height: '100vh', overflow: 'hidden', display: 'flex', flexDirection: 'column',
    }}>
      {/* ── TOPBAR (IRONSight glass-morphic) ── */}
      <div style={{
        height: 56, background: 'rgba(14, 17, 23, 0.82)',
        backdropFilter: 'blur(20px) saturate(1.4)',
        WebkitBackdropFilter: 'blur(20px) saturate(1.4)',
        borderBottom: `1px solid ${T.border}`,
        borderTop: '1px solid rgba(232, 115, 42, 0.15)',
        display: 'flex', alignItems: 'center', padding: '0 20px', gap: 20, flexShrink: 0,
        boxShadow: '0 1px 0 rgba(232, 115, 42, 0.06), 0 4px 20px rgba(0, 0, 0, 0.3)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Logo height={18} />
          <span style={{ fontWeight: 300, color: T.text2, fontSize: 14 }}>Search</span>
        </div>
        <div style={{ display: 'flex', gap: 2, marginLeft: 24 }}>
          <Link href="/operator" style={{ textDecoration: 'none' }}>
            <button style={{ padding: '6px 14px', fontSize: 12, background: 'none', border: 'none', color: T.text2, cursor: 'pointer', fontFamily: 'inherit', borderRadius: 4 }}>SOC Monitor</button>
          </Link>
          <Link href="/portal" style={{ textDecoration: 'none' }}>
            <button style={{ padding: '6px 14px', fontSize: 12, background: 'none', border: 'none', color: T.text2, cursor: 'pointer', fontFamily: 'inherit', borderRadius: 4 }}>Portal</button>
          </Link>
          <button style={{ padding: '6px 14px', fontSize: 12, background: `${T.accentGlow}`, border: `1px solid ${T.accentBorder}`, color: T.accent, cursor: 'pointer', fontFamily: 'inherit', borderRadius: 4, fontWeight: 600 }}>Search</button>
          <Link href="/" style={{ textDecoration: 'none' }}>
            <button style={{ padding: '6px 14px', fontSize: 12, background: 'none', border: 'none', color: T.text2, cursor: 'pointer', fontFamily: 'inherit', borderRadius: 4 }}>NVR</button>
          </Link>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 12, alignItems: 'center' }}>
          <span style={{ fontSize: 10, color: T.text2, padding: '3px 8px', background: T.bg3, border: `1px solid ${T.border}`, borderRadius: 10 }}>📷 762 cameras</span>
          <span style={{ fontSize: 10, color: T.red, padding: '3px 8px', background: 'rgba(255,92,72,0.1)', border: '1px solid rgba(255,92,72,0.25)', borderRadius: 10 }}>⚠ 9 incidents</span>
        </div>
      </div>

      {/* ── SEARCH BAR ── */}
      <div style={{ padding: '24px 20px 16px', background: T.bg, flexShrink: 0 }}>
        <div style={{
          display: 'flex', alignItems: 'center', gap: 12,
          padding: '12px 16px', background: T.bg2,
          border: `1px solid ${query ? T.accentBorder : T.border2}`,
          borderRadius: 10, transition: 'border-color 0.2s',
          boxShadow: query ? `0 0 20px ${T.accentGlow}` : undefined,
        }}>
          <span style={{ fontSize: 18, color: T.accent }}>🔍</span>
          <input
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleSearch()}
            placeholder="Search video: 'worker without hard hat near crane'..."
            style={{
              flex: 1, background: 'none', border: 'none', outline: 'none',
              color: T.text, fontSize: 15, fontFamily: "'Inter', sans-serif", fontWeight: 300,
            }}
          />
          <span style={{ fontSize: 10, color: T.text2, background: T.bg3, border: `1px solid ${T.border}`, padding: '2px 6px', borderRadius: 3, fontFamily: "'JetBrains Mono', monospace" }}>⌘K</span>
          <button
            onClick={handleSearch}
            disabled={loading}
            style={{
              padding: '8px 20px', background: T.accent, border: 'none', borderRadius: 6,
              color: '#fff', fontSize: 12, fontWeight: 700, cursor: 'pointer',
              fontFamily: 'inherit', boxShadow: `0 2px 12px ${T.accentGlow}`,
              opacity: loading ? 0.6 : 1,
            }}
          >
            {loading ? 'Searching…' : 'Search'}
          </button>
          <button onClick={handleSaveSearch} title="Save this search" style={{
            padding: '8px 12px', background: T.bg3, border: `1px solid ${T.border}`,
            borderRadius: 6, color: T.text2, fontSize: 12, cursor: 'pointer', fontFamily: 'inherit',
          }}>💾</button>
          <div style={{ position: 'relative' }}>
            <button onClick={() => setShowSaved(v => !v)} title="Saved searches" style={{
              padding: '8px 12px', background: showSaved ? T.accentGlow : T.bg3,
              border: `1px solid ${showSaved ? T.accentBorder : T.border}`,
              borderRadius: 6, color: showSaved ? T.accent : T.text2, fontSize: 12,
              cursor: 'pointer', fontFamily: 'inherit',
            }}>📌 {savedSearches.length}</button>
            {showSaved && (
              <div style={{
                position: 'absolute', top: '100%', right: 0, marginTop: 4, width: 320,
                background: T.bg2, border: `1px solid ${T.border2}`, borderRadius: 8,
                boxShadow: '0 12px 40px rgba(0,0,0,0.5)', zIndex: 50, maxHeight: 300, overflowY: 'auto',
                scrollbarWidth: 'thin' as const,
              }}>
                <div style={{ padding: '8px 12px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, borderBottom: `1px solid ${T.border}` }}>
                  Saved Searches
                </div>
                {savedSearches.map(s => (
                  <div key={s.id} style={{ display: 'flex', alignItems: 'center', padding: '6px 12px', borderBottom: `1px solid ${T.border}`, cursor: 'pointer' }} onClick={() => handleLoadSaved(s)}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 11, fontWeight: 500, color: T.text, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const }}>{s.name}</div>
                      <div style={{ fontSize: 9, color: T.text2 }}>{s.shared ? '🌐 Shared' : '🔒 Private'} · {s.run_count} runs</div>
                    </div>
                    <button onClick={e => { e.stopPropagation(); handleDeleteSaved(s.id); }} style={{ background: 'none', border: 'none', color: T.text2, cursor: 'pointer', fontSize: 10, padding: '2px 4px' }}>✕</button>
                  </div>
                ))}
                {savedSearches.length === 0 && (
                  <div style={{ padding: 16, textAlign: 'center', fontSize: 11, color: T.text2 }}>No saved searches yet</div>
                )}
              </div>
            )}
          </div>
        </div>

      </div>

      {/* cleanCaption extracts a readable description when the VLM returned
          its full JSON response as a single string (early-indexer artifact). */}
      {/* ── MAIN: LEFT REFINEMENTS + RESULTS + PREVIEW ── */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'grid', gridTemplateColumns: selected ? '220px 1fr 380px' : '220px 1fr' }}>
        {/* ── LEFT REFINEMENT PANEL ── */}
        <div style={{
          borderRight: `1px solid ${T.border}`, background: T.bg2,
          overflowY: 'auto', scrollbarWidth: 'thin' as const, padding: '14px 12px',
          display: 'flex', flexDirection: 'column', gap: 16,
        }}>
          {/* Violation Types */}
          <div>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, marginBottom: 8 }}>Violation Type</div>
            {Object.entries(violationTypes).map(([type, active]) => (
              <label
                key={type}
                style={{
                  display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0',
                  cursor: 'pointer', fontSize: 11, color: active ? T.accent : T.text2,
                }}
              >
                <input
                  type="checkbox"
                  checked={active}
                  onChange={() => toggleViolation(type)}
                  style={{ accentColor: T.accent }}
                />
                {type.replace(/_/g, ' ')}
              </label>
            ))}
          </div>

          {/* Site Selector */}
          <div>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, marginBottom: 8 }}>Site</div>
            <select
              value={(filters.site_ids || [])[0] || ''}
              onChange={e => updateFilters({ site_ids: e.target.value ? [e.target.value] : [] })}
              style={{
                width: '100%', padding: '6px 8px', borderRadius: 4, fontSize: 11,
                background: T.bg3, border: `1px solid ${T.border}`,
                color: T.text, fontFamily: 'inherit',
              }}
            >
              <option value="">All Sites</option>
              {sitesData.map(site => (
                <option key={site.id} value={site.id}>
                  {site.name || site.id}
                </option>
              ))}
            </select>
          </div>

          {/* Date Range */}
          <div>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, marginBottom: 8 }}>Date Range</div>
            <input
              type="date"
              value={filters.date_range?.start || ''}
              onChange={e => updateFilters({ date_range: { start: e.target.value, end: filters.date_range?.end || '' } })}
              style={{
                width: '100%', padding: '5px 8px', borderRadius: 4, fontSize: 10,
                background: T.bg3, border: `1px solid ${T.border}`,
                color: T.text, fontFamily: 'inherit', marginBottom: 4,
              }}
            />
            <input
              type="date"
              value={filters.date_range?.end || ''}
              onChange={e => updateFilters({ date_range: { start: filters.date_range?.start || '', end: e.target.value } })}
              style={{
                width: '100%', padding: '5px 8px', borderRadius: 4, fontSize: 10,
                background: T.bg3, border: `1px solid ${T.border}`,
                color: T.text, fontFamily: 'inherit',
              }}
            />
          </div>

          {/* Confidence Slider */}
          <div>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, marginBottom: 8 }}>
              Min Confidence <span style={{ color: T.accent, fontFamily: "'JetBrains Mono', monospace" }}>{filters.confidence_min ? `${Math.round(filters.confidence_min * 100)}%` : 'Any'}</span>
            </div>
            <input
              type="range"
              min={0} max={100} step={5}
              value={(filters.confidence_min || 0) * 100}
              onChange={e => updateFilters({ confidence_min: Number(e.target.value) / 100 || undefined })}
              style={{ width: '100%', accentColor: T.accent }}
            />
          </div>

          {/* Time of Day */}
          <div>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, marginBottom: 8 }}>Time of Day</div>
            <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
              <input
                type="time"
                value={filters.time_of_day?.start || ''}
                onChange={e => updateFilters({ time_of_day: { start: e.target.value, end: filters.time_of_day?.end || '23:59' } })}
                style={{
                  flex: 1, padding: '5px 6px', borderRadius: 4, fontSize: 10,
                  background: T.bg3, border: `1px solid ${T.border}`,
                  color: T.text, fontFamily: 'inherit',
                }}
              />
              <span style={{ color: T.text2, fontSize: 10 }}>–</span>
              <input
                type="time"
                value={filters.time_of_day?.end || ''}
                onChange={e => updateFilters({ time_of_day: { start: filters.time_of_day?.start || '00:00', end: e.target.value } })}
                style={{
                  flex: 1, padding: '5px 6px', borderRadius: 4, fontSize: 10,
                  background: T.bg3, border: `1px solid ${T.border}`,
                  color: T.text, fontFamily: 'inherit',
                }}
              />
            </div>
          </div>

          {/* Model Selector */}
          <div>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: T.text2, marginBottom: 8 }}>Search Model</div>
            {(['hybrid', 'visual', 'caption'] as const).map(m => (
              <label
                key={m}
                style={{
                  display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0',
                  cursor: 'pointer', fontSize: 11, color: filters.model === m ? T.accent : T.text2,
                }}
              >
                <input
                  type="radio"
                  name="model"
                  checked={filters.model === m}
                  onChange={() => updateFilters({ model: m })}
                  style={{ accentColor: T.accent }}
                />
                {m === 'hybrid' ? 'Hybrid' : m === 'visual' ? 'Visual Only' : 'Caption Only'}
              </label>
            ))}
          </div>

          {/* Reset */}
          <button
            onClick={() => { useSearchStore.getState().resetFilters(); }}
            style={{
              padding: '6px 12px', borderRadius: 4, fontSize: 10, fontWeight: 600,
              background: T.bg3, border: `1px solid ${T.border}`,
              color: T.text2, cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            Reset Filters
          </button>
        </div>

        {/* Results grid */}
        <div style={{ overflowY: 'auto', padding: '0 20px 20px', scrollbarWidth: 'thin' as const, scrollbarColor: `${T.border} transparent` }}>
          {!hasSearched && (
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: T.text2, gap: 12 }}>
              <span style={{ fontSize: 40 }}>🔍</span>
              <div style={{ fontSize: 16, fontWeight: 300 }}>Search across all video feeds using natural language</div>
              <div style={{ fontSize: 12, color: T.text2 }}>Try: "worker without hard hat near crane" or "person in exclusion zone"</div>
            </div>
          )}

          {hasSearched && results.length === 0 && (
            <div style={{ textAlign: 'center', padding: 60, color: T.text2, fontSize: 14 }}>
              No results found for "{query}"
            </div>
          )}

          {results.length > 0 && (
            <>
              <div style={{ fontSize: 12, color: T.text2, marginBottom: 12, padding: '4px 0' }}>
                {results.length} results for "<strong style={{ color: T.text }}>{query}</strong>"
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 10 }}>
                {results.map((res, idx) => (
                  <div
                    key={res.frame_id}
                    onClick={() => selectResult(res.frame_id)}
                    style={{
                      borderRadius: 8, overflow: 'hidden',
                      border: `1px solid ${selectedResultId === res.frame_id ? T.accentBorder : T.border}`,
                      background: T.bg2, cursor: 'pointer', transition: 'all 0.2s',
                      boxShadow: selectedResultId === res.frame_id ? `0 0 16px ${T.accentGlow}` : undefined,
                    }}
                  >
                    {/* Thumbnail — uses the clip itself as its own poster.
                        preload="metadata" asks the browser to decode just
                        enough to render frame 0 as a still; muted is a
                        Chrome autoplay-policy requirement so the browser
                        will actually fetch the frame without user gesture.
                        Falls back to the scene gradient when no clip exists. */}
                    <div style={{ aspectRatio: '16/9', position: 'relative', background: sceneBgs[idx % sceneBgs.length], overflow: 'hidden' }}>
                      {res.clip_url && (
                        <video
                          src={res.clip_url}
                          poster={res.thumbnail_url || undefined}
                          preload="metadata"
                          muted
                          playsInline
                          // Some browsers ignore #t= media fragments for
                          // preload=metadata and render frame 0. Force the
                          // seek once metadata lands so the poster frame
                          // actually matches the event moment.
                          onLoadedMetadata={(e) => {
                            const m = res.clip_url.match(/#t=([\d.]+)/);
                            if (m) { (e.currentTarget as HTMLVideoElement).currentTime = parseFloat(m[1]); }
                          }}
                          style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' }}
                        />
                      )}
                      {/* Relevance badge */}
                      <span style={{
                        position: 'absolute', top: 6, right: 6,
                        fontFamily: "'JetBrains Mono', monospace", fontSize: 9, fontWeight: 700,
                        padding: '2px 6px', borderRadius: 3,
                        background: res.relevance_score >= 0.9 ? 'rgba(62,207,142,0.25)' : res.relevance_score >= 0.7 ? 'rgba(108,143,255,0.25)' : 'rgba(255,179,64,0.25)',
                        border: `1px solid ${res.relevance_score >= 0.9 ? 'rgba(62,207,142,0.4)' : res.relevance_score >= 0.7 ? T.accentBorder : 'rgba(255,179,64,0.4)'}`,
                        color: res.relevance_score >= 0.9 ? T.green : res.relevance_score >= 0.7 ? T.accent : T.amber,
                        zIndex: 1,
                      }}>
                        {Math.round(res.relevance_score * 100)}%
                      </span>

                      {/* Violation indicator */}
                      {Object.keys(res.violation_flags).length > 0 && (
                        <span style={{
                          position: 'absolute', top: 6, left: 6,
                          fontSize: 8, padding: '2px 5px', borderRadius: 2,
                          background: 'rgba(255,92,72,0.25)', border: '1px solid rgba(255,92,72,0.4)',
                          color: T.red, fontWeight: 700, letterSpacing: 0.5,
                          fontFamily: "'JetBrains Mono', monospace",
                          zIndex: 1,
                        }}>
                          ⚠ VIOLATION
                        </span>
                      )}

                      {/* Detection box overlay.
                          Show every detection, not just a slice — a 3rd box is often
                          the one the user searched for (YOLO's "truck" class covers
                          vans). Class-name-matching-query boxes get the accent color
                          and a label so users can tell WHICH object matched. */}
                      {res.detections.map((det, di) => {
                        const isQueryMatch = det.class &&
                          query.toLowerCase().split(/\s+/).some(w => w && det.class.toLowerCase().includes(w));
                        const color = det.violation ? T.red : isQueryMatch ? T.accent : T.green;
                        return (
                          <div key={di} style={{
                            position: 'absolute',
                            left: `${(det.bbox[0] / 1920) * 100}%`,
                            top: `${(det.bbox[1] / 1080) * 100}%`,
                            width: `${((det.bbox[2] - det.bbox[0]) / 1920) * 100}%`,
                            height: `${((det.bbox[3] - det.bbox[1]) / 1080) * 100}%`,
                            border: `1.5px solid ${color}`,
                            boxShadow: isQueryMatch ? `0 0 10px ${T.accentGlow}` : undefined,
                            borderRadius: 2, pointerEvents: 'none', zIndex: 1,
                          }}>
                            {det.class && (
                              <span style={{
                                position: 'absolute', top: -2, left: -2, transform: 'translateY(-100%)',
                                fontFamily: "'JetBrains Mono', monospace", fontSize: 8, fontWeight: 700,
                                padding: '1px 4px', borderRadius: 2,
                                background: color, color: '#000',
                                whiteSpace: 'nowrap',
                              }}>
                                {det.class}
                              </span>
                            )}
                          </div>
                        );
                      })}
                    </div>

                    {/* Info */}
                    <div style={{ padding: '8px 10px' }}>
                      <div style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 9, color: T.text2 }}>
                        {res.site_name} · {res.camera_name}
                      </div>
                      <div style={{
                        fontSize: 11, color: T.text, fontWeight: 400,
                        lineHeight: 1.3, marginTop: 3,
                        whiteSpace: 'nowrap' as const, overflow: 'hidden', textOverflow: 'ellipsis',
                      }}>
                        {cleanCaption(res.caption)}
                      </div>
                      <div style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 9, color: T.text2, marginTop: 3 }}>
                        {formatRelativeTime(res.ts)}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>

        {/* ── PREVIEW PANE ── */}
        {selected && (
          <div style={{
            borderLeft: `1px solid ${T.border}`, background: T.bg2,
            overflowY: 'auto', scrollbarWidth: 'thin' as const,
          }}>
            {/* Preview video — plays the recorded segment from clip_url with
                detection boxes overlaid. Falls back to the thumbnail scene
                when no clip_url is present (e.g. older rows or alarm-only
                results missing a segment link). */}
            <div style={{ position: 'relative', aspectRatio: '16/9', background: '#000' }}>
              {selected.clip_url ? (
                <video
                  key={selected.frame_id}
                  src={selected.clip_url}
                  poster={selected.thumbnail_url || undefined}
                  controls
                  autoPlay
                  muted
                  style={{ width: '100%', height: '100%', objectFit: 'contain', background: '#000' }}
                />
              ) : (
                <div style={{
                  position: 'absolute', inset: 0,
                  background: sceneBgs[(selectedIdx >= 0 ? selectedIdx : 0) % sceneBgs.length],
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  color: T.text2, fontSize: 11,
                }}>
                  No clip available for this result
                </div>
              )}
              {selected.detections.map((det, di) => (
                <div key={di} style={{
                  position: 'absolute',
                  left: `${(det.bbox[0] / 1920) * 100}%`,
                  top: `${(det.bbox[1] / 1080) * 100}%`,
                  width: `${((det.bbox[2] - det.bbox[0]) / 1920) * 100}%`,
                  height: `${((det.bbox[3] - det.bbox[1]) / 1080) * 100}%`,
                  border: `2px solid ${det.violation ? T.red : T.green}`,
                  borderRadius: 2, pointerEvents: 'none',
                }} />
              ))}
            </div>

            <div style={{ padding: '16px 18px', display: 'flex', flexDirection: 'column', gap: 14 }}>
              {/* Relevance score bar */}
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: T.text2, marginBottom: 4 }}>
                  <span>Relevance Score</span>
                  <span style={{ color: T.accent, fontWeight: 600, fontFamily: "'JetBrains Mono', monospace" }}>{Math.round(selected.relevance_score * 100)}%</span>
                </div>
                <div style={{ height: 6, background: T.bg3, borderRadius: 3, overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: `${selected.relevance_score * 100}%`, background: `linear-gradient(90deg, ${T.accent}, ${T.purple})`, borderRadius: 3, transition: 'width 0.4s' }} />
                </div>
              </div>

              {/* Caption */}
              <div>
                <div style={{ fontSize: 10, fontWeight: 600, color: T.text2, letterSpacing: 1, textTransform: 'uppercase' as const, marginBottom: 6 }}>Caption</div>
                <div style={{ fontSize: 12, color: T.text, lineHeight: 1.5, padding: '10px 12px', background: T.bg3, borderRadius: 6, border: `1px solid ${T.border}` }}>
                  {cleanCaption(selected.caption)}
                </div>
              </div>

              {/* Meta */}
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                <div style={{ padding: '8px 10px', background: T.bg3, borderRadius: 6, border: `1px solid ${T.border}` }}>
                  <div style={{ fontSize: 9, color: T.text2, marginBottom: 2 }}>SITE</div>
                  <div style={{ fontSize: 11, fontWeight: 500 }}>{selected.site_name}</div>
                </div>
                <div style={{ padding: '8px 10px', background: T.bg3, borderRadius: 6, border: `1px solid ${T.border}` }}>
                  <div style={{ fontSize: 9, color: T.text2, marginBottom: 2 }}>CAMERA</div>
                  <div style={{ fontSize: 11, fontWeight: 500 }}>{selected.camera_name}</div>
                </div>
              </div>

              {/* Token matches */}
              {selected.token_matches.length > 0 && (
                <div>
                  <div style={{ fontSize: 10, fontWeight: 600, color: T.text2, letterSpacing: 1, textTransform: 'uppercase' as const, marginBottom: 6 }}>Token Matches</div>
                  {selected.token_matches.map((tm, i) => (
                    <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                      <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 11, color: T.accent, fontWeight: 500, flex: 1 }}>"{tm.token}"</span>
                      <span style={{ fontSize: 9, color: T.text2, padding: '1px 6px', background: T.bg3, border: `1px solid ${T.border}`, borderRadius: 10 }}>{tm.source}</span>
                      <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 10, color: T.green, fontWeight: 600 }}>{Math.round(tm.score * 100)}%</span>
                    </div>
                  ))}
                </div>
              )}

              {/* Actions */}
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  onClick={() => {
                    if (!selected.clip_url) return;
                    // Restart the <video> element in the preview pane. Small
                    // ergonomic win — user scrolled down after auto-play ended
                    // and now wants to watch again.
                    const v = document.querySelector('video') as HTMLVideoElement | null;
                    if (v) {
                      v.currentTime = 0;
                      v.play();
                      v.scrollIntoView({ behavior: 'smooth', block: 'start' });
                    }
                  }}
                  disabled={!selected.clip_url}
                  style={{
                    flex: 1, padding: '8px 12px', borderRadius: 6, fontSize: 11, fontWeight: 600,
                    background: selected.clip_url ? T.accentGlow : T.bg3,
                    border: `1px solid ${selected.clip_url ? T.accentBorder : T.border}`,
                    color: selected.clip_url ? T.accent : T.text2,
                    cursor: selected.clip_url ? 'pointer' : 'not-allowed',
                    fontFamily: 'inherit',
                  }}
                >▶ Play Clip</button>
                {/* Open this result's site in the portal's Incidents page in
                    a new tab. Filing happens on the customer side, not here;
                    this link is a fast bridge for operators reviewing a
                    match in the search view. */}
                <Link
                  href={selected.site_id ? `/portal/sites/${selected.site_id}` : '/portal'}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{
                    flex: 1, padding: '8px 12px', borderRadius: 6, fontSize: 11, fontWeight: 600,
                    background: T.bg3, border: `1px solid ${T.border}`, color: T.text2,
                    cursor: 'pointer', fontFamily: 'inherit', textDecoration: 'none',
                    textAlign: 'center' as const,
                  }}
                >📌 Open in Portal</Link>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
