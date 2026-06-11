'use client';

// Live VCA zone overlay. Renders a camera's configured VCA detection
// zones/polygons over the LIVE <video> in VideoPlayer, toggleable via
// useVCAZonesToggle. The SVG rendering is ported from
// operator/AlarmVideoFeed.tsx (the alarm-tile zone overlay) — same palette,
// same polygon/line shapes — but adapted for the always-on live view:
// every enabled zone is drawn equally (no per-alarm "TRIGGERED" highlight),
// each labelled with its rule name, and tripwires (linecross) get a small
// direction arrow.
//
// COORDINATE MODEL — why this aligns with the video:
//   VCA region points are normalized 0.0–1.0. We render them into a
//   viewBox="0 0 100 100" user space (x*100, y*100). The live <video> uses
//   `object-fit: contain` (operator.css `.video-cell video`), which
//   letterboxes the frame inside the cell. preserveAspectRatio="xMidYMid
//   meet" makes this SVG letterbox its 100×100 user space *identically*, so
//   the normalized coords land on the painted video rect (not the cell box)
//   regardless of camera vs cell aspect ratio — matching whatever the video
//   element paints. The wrapper that hosts this SVG carries the same
//   transform (translate+scale) as the <video> so zones track digital zoom.

import type { VCARule } from '@/lib/api';

// Palette consistent with AlarmVideoFeed.tsx / the VCA zone editor.
const ZONE_FILL: Record<string, string> = {
    intrusion: 'rgba(239,68,68,0.15)',
    linecross: 'rgba(59,130,246,0.15)',
    regionentrance: 'rgba(34,197,94,0.15)',
    loitering: 'rgba(234,179,8,0.15)',
};
const ZONE_STROKE: Record<string, string> = {
    intrusion: '#EF4444',
    linecross: '#3B82F6',
    regionentrance: '#22C55E',
    loitering: '#EAB308',
};

interface VCAZoneOverlayProps {
    rules: VCARule[];
}

export default function VCAZoneOverlay({ rules }: VCAZoneOverlayProps) {
    const drawable = rules.filter(
        r => r.enabled && Array.isArray(r.region) && r.region.length >= 2,
    );
    if (drawable.length === 0) return null;

    return (
        <svg
            viewBox="0 0 100 100"
            preserveAspectRatio="xMidYMid meet"
            style={{
                position: 'absolute',
                inset: 0,
                width: '100%',
                height: '100%',
                pointerEvents: 'none',
                // Above the video + scanline (z-index:1), below the header (z-index:2).
            }}
        >
            {drawable.map(rule => {
                const stroke = ZONE_STROKE[rule.rule_type] || '#8891A5';
                const fill = ZONE_FILL[rule.rule_type] || 'rgba(255,255,255,0.05)';

                // Tripwire: a 2-point line with a direction arrow.
                if (rule.rule_type === 'linecross' && rule.region.length === 2) {
                    const [a, b] = rule.region;
                    const x1 = a.x * 100, y1 = a.y * 100;
                    const x2 = b.x * 100, y2 = b.y * 100;
                    const mx = (x1 + x2) / 2, my = (y1 + y2) / 2;
                    // Arrow: perpendicular to the line, pointing in the configured
                    // direction. 'both' -> double-headed; one-way -> single.
                    const dx = x2 - x1, dy = y2 - y1;
                    const len = Math.hypot(dx, dy) || 1;
                    // Unit perpendicular (rotate the line dir 90°).
                    const px = -dy / len, py = dx / len;
                    const arrowLen = 8;
                    const dir = rule.direction;
                    // left_to_right follows the perpendicular +; right_to_left the -.
                    const sign = dir === 'right_to_left' ? -1 : 1;
                    const ax = mx + px * arrowLen * sign;
                    const ay = my + py * arrowLen * sign;
                    return (
                        <g key={rule.id}>
                            <line
                                x1={x1} y1={y1} x2={x2} y2={y2}
                                stroke={stroke} strokeWidth={1.2} strokeDasharray="4 2"
                                vectorEffect="non-scaling-stroke"
                            />
                            {/* Direction arrow shaft from the midpoint. */}
                            <line
                                x1={mx} y1={my} x2={ax} y2={ay}
                                stroke={stroke} strokeWidth={1} markerEnd={`url(#vca-arrow-${rule.id})`}
                                vectorEffect="non-scaling-stroke"
                            />
                            {dir === 'both' && (
                                <line
                                    x1={mx} y1={my}
                                    x2={mx - px * arrowLen} y2={my - py * arrowLen}
                                    stroke={stroke} strokeWidth={1} markerEnd={`url(#vca-arrow-${rule.id})`}
                                    vectorEffect="non-scaling-stroke"
                                />
                            )}
                            <defs>
                                <marker
                                    id={`vca-arrow-${rule.id}`}
                                    markerWidth={6} markerHeight={6} refX={4} refY={3}
                                    orient="auto" markerUnits="userSpaceOnUse"
                                >
                                    <path d="M0,0 L5,3 L0,6 Z" fill={stroke} />
                                </marker>
                            </defs>
                            <text
                                x={mx} y={my} dy={-3}
                                fill={stroke} fontSize={4} fontWeight={600} textAnchor="middle"
                                style={{ fontFamily: "'JetBrains Mono', monospace", paintOrder: 'stroke' }}
                                stroke="rgba(0,0,0,0.6)" strokeWidth={0.4}
                            >
                                {rule.name}
                            </text>
                        </g>
                    );
                }

                // Intrusion / region-entrance / loitering: a filled polygon.
                const pts = rule.region
                    .map(p => `${(p.x * 100).toFixed(2)},${(p.y * 100).toFixed(2)}`)
                    .join(' ');
                const cx = (rule.region.reduce((s, p) => s + p.x, 0) / rule.region.length) * 100;
                const cy = (rule.region.reduce((s, p) => s + p.y, 0) / rule.region.length) * 100;
                return (
                    <g key={rule.id}>
                        <polygon
                            points={pts}
                            fill={fill}
                            stroke={stroke}
                            strokeWidth={1.2}
                            strokeDasharray="4 2"
                            vectorEffect="non-scaling-stroke"
                        />
                        <text
                            x={cx} y={cy}
                            fill={stroke} fontSize={4} fontWeight={600}
                            textAnchor="middle" dominantBaseline="middle"
                            style={{ fontFamily: "'JetBrains Mono', monospace", paintOrder: 'stroke' }}
                            stroke="rgba(0,0,0,0.6)" strokeWidth={0.4}
                        >
                            {rule.name}
                        </text>
                    </g>
                );
            })}
        </svg>
    );
}
