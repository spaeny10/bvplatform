'use client';

// PTZ control overlay — directional buttons + zoom in/out. Extracted
// from VideoPlayer.tsx (P1-B-11 session 6). The parent owns the
// hold-to-move handlers (handlePTZStart/handlePTZStop) so this child
// stays stateless; rendering this in a separate file gets ~56 lines of
// visual JSX out of the main video-pipeline component.

interface Props {
    /** Hold-to-move handler: positive values = right/up/zoom-in. */
    onStart: (pan: number, tilt: number, zoom: number) => void;
    onStop: () => void;
}

export default function PTZOverlay({ onStart, onStop }: Props) {
    return (
        <div style={{
            position: 'absolute',
            bottom: '8px',
            right: '8px',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: '4px',
            zIndex: 10,
            background: 'rgba(0,0,0,0.5)',
            padding: '6px',
            borderRadius: '8px',
        }}>
            <div style={{ display: 'flex', gap: '4px', justifyContent: 'center' }}>
                {/* Pan left */}
                <button className="ptz-btn"
                    onMouseDown={() => onStart(-1, 0, 0)} onMouseUp={onStop}
                    onTouchStart={() => onStart(-1, 0, 0)} onTouchEnd={onStop}
                    onMouseLeave={onStop}>◀</button>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                    {/* Tilt up */}
                    <button className="ptz-btn"
                        onMouseDown={() => onStart(0, 1, 0)} onMouseUp={onStop}
                        onTouchStart={() => onStart(0, 1, 0)} onTouchEnd={onStop}
                        onMouseLeave={onStop}>▲</button>
                    {/* Tilt down */}
                    <button className="ptz-btn"
                        onMouseDown={() => onStart(0, -1, 0)} onMouseUp={onStop}
                        onTouchStart={() => onStart(0, -1, 0)} onTouchEnd={onStop}
                        onMouseLeave={onStop}>▼</button>
                </div>

                {/* Pan right */}
                <button className="ptz-btn"
                    onMouseDown={() => onStart(1, 0, 0)} onMouseUp={onStop}
                    onTouchStart={() => onStart(1, 0, 0)} onTouchEnd={onStop}
                    onMouseLeave={onStop}>▶</button>
            </div>

            <div style={{ display: 'flex', gap: '4px', width: '100%', marginTop: '4px', borderTop: '1px solid rgba(255,255,255,0.2)', paddingTop: '4px' }}>
                {/* Zoom out */}
                <button className="ptz-btn" style={{ flex: 1 }}
                    onMouseDown={() => onStart(0, 0, -1)} onMouseUp={onStop}
                    onTouchStart={() => onStart(0, 0, -1)} onTouchEnd={onStop}
                    onMouseLeave={onStop}>-</button>
                {/* Zoom in */}
                <button className="ptz-btn" style={{ flex: 1 }}
                    onMouseDown={() => onStart(0, 0, 1)} onMouseUp={onStop}
                    onTouchStart={() => onStart(0, 0, 1)} onTouchEnd={onStop}
                    onMouseLeave={onStop}>+</button>
            </div>
        </div>
    );
}
