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
//   viewBox="0 0 frameW frameH" user space (x*frameW, y*frameH) whose aspect
//   ratio EQUALS the camera frame's aspect ratio. The live <video> uses
//   `object-fit: contain` (operator.css `.video-cell video`), which
//   letterboxes the frame inside the cell. preserveAspectRatio="xMidYMid
//   meet" makes this SVG letterbox its frameW×frameH user space *identically*
//   to the video's contain-letterbox (same aspect → same fitted rect), so the
//   normalized coords land on the painted video rect (not the cell box) for
//   every camera aspect — including panoramics (~3.37:1) and PTZs (~1.22:1).
//   A square viewBox (the old 100×100) only matched square cameras and offset
//   the zones on everything else. The wrapper that hosts this SVG carries the
//   same transform (translate+scale) as the <video> so zones track digital
//   zoom.

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
    // Intrinsic video frame dimensions (video.videoWidth/Height). The viewBox
    // adopts these so its aspect == the camera aspect, making the SVG's meet
    // letterbox coincide with the video's object-fit:contain letterbox.
    frameW: number;
    frameH: number;
}

export default function VCAZoneOverlay({ rules, frameW, frameH }: VCAZoneOverlayProps) {
    // Unknown frame dims → we can't build an aspect-correct viewBox, so don't
    // draw (a wrong-aspect overlay is worse than none).
    if (frameW <= 0 || frameH <= 0) return null;

    const drawable = rules.filter(
        r => r.enabled && Array.isArray(r.region) && r.region.length >= 2,
    );
    if (drawable.length === 0) return null;

    // User-space scale factor. The original sizes were tuned for a 100-unit
    // tall space; scale them by frameH/100 so text/arrows keep the same visual
    // proportion now that the viewBox is frameH units tall. (Polygon/line
    // strokes use vectorEffect="non-scaling-stroke" → screen px, so they need
    // no scaling here.)
    const u = frameH / 100;

    return (
        <svg
            viewBox={`0 0 ${frameW} ${frameH}`}
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
                    const x1 = a.x * frameW, y1 = a.y * frameH;
                    const x2 = b.x * frameW, y2 = b.y * frameH;
                    const mx = (x1 + x2) / 2, my = (y1 + y2) / 2;
                    // Arrow: perpendicular to the line, pointing in the configured
                    // direction. 'both' -> double-headed; one-way -> single.
                    const dx = x2 - x1, dy = y2 - y1;
                    const len = Math.hypot(dx, dy) || 1;
                    // Unit perpendicular (rotate the line dir 90°).
                    const px = -dy / len, py = dx / len;
                    const arrowLen = 8 * u;
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
                                    markerWidth={6 * u} markerHeight={6 * u} refX={4 * u} refY={3 * u}
                                    orient="auto" markerUnits="userSpaceOnUse"
                                >
                                    <path d={`M0,0 L${5 * u},${3 * u} L0,${6 * u} Z`} fill={stroke} />
                                </marker>
                            </defs>
                            <text
                                x={mx} y={my} dy={-3 * u}
                                fill={stroke} fontSize={4 * u} fontWeight={600} textAnchor="middle"
                                style={{ fontFamily: "'JetBrains Mono', monospace", paintOrder: 'stroke' }}
                                stroke="rgba(0,0,0,0.6)" strokeWidth={0.4 * u}
                            >
                                {rule.name}
                            </text>
                        </g>
                    );
                }

                // Intrusion / region-entrance / loitering: a filled polygon.
                const pts = rule.region
                    .map(p => `${(p.x * frameW).toFixed(2)},${(p.y * frameH).toFixed(2)}`)
                    .join(' ');
                const cx = (rule.region.reduce((s, p) => s + p.x, 0) / rule.region.length) * frameW;
                const cy = (rule.region.reduce((s, p) => s + p.y, 0) / rule.region.length) * frameH;
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
                            fill={stroke} fontSize={4 * u} fontWeight={600}
                            textAnchor="middle" dominantBaseline="middle"
                            style={{ fontFamily: "'JetBrains Mono', monospace", paintOrder: 'stroke' }}
                            stroke="rgba(0,0,0,0.6)" strokeWidth={0.4 * u}
                        >
                            {rule.name}
                        </text>
                    </g>
                );
            })}
        </svg>
    );
}
