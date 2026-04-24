'use client';

import type { Detection } from '@/types/ironsight';

interface DetectionOverlayProps {
  detections: Detection[];
  containerWidth: number;
  containerHeight: number;
  sourceWidth?: number;   // pixel width of original frame
  sourceHeight?: number;  // pixel height of original frame
  showLabels?: boolean;
  showConfidence?: boolean;
}

const CLASS_COLORS: Record<string, string> = {
  person: '#40c080',       // green (compliant)
  no_harness: '#e05040',   // red
  no_hard_hat: '#e05040',
  no_hi_vis: '#e05040',
  zone_breach: '#e05040',
  vehicle: '#d4a030',      // amber
  excavator: '#d4a030',
  scaffold: '#a070d0',     // purple
  hard_hat: '#40c080',
  harness: '#40c080',
  hi_vis: '#40c080',
};

function getColor(det: Detection): string {
  if (det.violation) return '#e05040';
  return CLASS_COLORS[det.subclass || det.class] || '#40c080';
}

export default function DetectionOverlay({
  detections,
  containerWidth,
  containerHeight,
  sourceWidth = 1920,
  sourceHeight = 1080,
  showLabels = true,
  showConfidence = true,
}: DetectionOverlayProps) {
  const scaleX = containerWidth / sourceWidth;
  const scaleY = containerHeight / sourceHeight;

  return (
    <svg
      viewBox={`0 0 ${containerWidth} ${containerHeight}`}
      style={{
        position: 'absolute',
        inset: 0,
        width: '100%',
        height: '100%',
        pointerEvents: 'none',
        overflow: 'visible',
      }}
    >
      {detections.map((det, i) => {
        const [x1, y1, x2, y2] = det.bbox;
        const sx = x1 * scaleX;
        const sy = y1 * scaleY;
        const sw = (x2 - x1) * scaleX;
        const sh = (y2 - y1) * scaleY;
        const color = getColor(det);
        const label = det.subclass || det.class;
        const conf = Math.round(det.confidence * 100);

        return (
          <g key={i}>
            {/* Bounding box */}
            <rect
              x={sx} y={sy} width={sw} height={sh}
              fill="none"
              stroke={color}
              strokeWidth={2}
              rx={2}
              style={det.violation && det.in_exclusion_zone ? {
                filter: `drop-shadow(0 0 6px ${color})`,
              } : undefined}
            />

            {/* Label */}
            {showLabels && (
              <g transform={`translate(${sx - 1}, ${sy - 18})`}>
                <rect
                  width={label.length * 6.5 + (showConfidence ? 30 : 8)}
                  height={16}
                  rx={2}
                  fill="rgba(0,0,0,0.85)"
                />
                <text
                  x={4} y={11}
                  fill={color}
                  fontSize={9}
                  fontFamily="'JetBrains Mono', 'Fira Code', monospace"
                  fontWeight={500}
                >
                  {label.toUpperCase()}{showConfidence ? ` ${conf}%` : ''}
                </text>
              </g>
            )}

            {/* Confidence bar */}
            {showConfidence && (
              <rect
                x={sx} y={sy + sh + 2}
                width={sw * det.confidence}
                height={2}
                rx={1}
                fill={color}
                opacity={0.7}
              />
            )}
          </g>
        );
      })}
    </svg>
  );
}
