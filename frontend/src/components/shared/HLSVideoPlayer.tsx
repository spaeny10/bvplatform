'use client';

import { useRef, useEffect, useState } from 'react';
import Hls from 'hls.js';

interface Props {
  src?: string;
  poster?: string;
  autoPlay?: boolean;
  muted?: boolean;
  style?: React.CSSProperties;
  onError?: () => void;
}

export default function HLSVideoPlayer({ src, poster, autoPlay = true, muted = true, style, onError }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [status, setStatus] = useState<'loading' | 'playing' | 'error' | 'no-src'>(!src ? 'no-src' : 'loading');

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !src) {
      setStatus('no-src');
      return;
    }

    setStatus('loading');

    // Native HLS support (Safari)
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = src;
      video.addEventListener('loadedmetadata', () => {
        if (autoPlay) video.play().catch(() => {});
        setStatus('playing');
      });
      video.addEventListener('error', () => {
        setStatus('error');
        onError?.();
      });
      return;
    }

    // hls.js for other browsers
    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: true,
        backBufferLength: 30,
      });
      hlsRef.current = hls;

      hls.loadSource(src);
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        if (autoPlay) video.play().catch(() => {});
        setStatus('playing');
      });

      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          setStatus('error');
          onError?.();
          if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
            hls.startLoad();
          } else {
            hls.destroy();
          }
        }
      });

      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    }

    setStatus('error');
  }, [src, autoPlay, onError]);

  return (
    <div style={{ position: 'relative', width: '100%', height: '100%', background: '#000', ...style }}>
      <video
        ref={videoRef}
        muted={muted}
        playsInline
        poster={poster}
        style={{ width: '100%', height: '100%', objectFit: 'cover', display: status === 'no-src' ? 'none' : 'block' }}
      />
      {status === 'no-src' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: '#4A5268', fontSize: 10, fontFamily: "'JetBrains Mono', monospace",
        }}>
          No stream available
        </div>
      )}
      {status === 'loading' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
          background: 'rgba(0,0,0,0.5)',
        }}>
          <div style={{
            width: 24, height: 24, border: '2px solid rgba(0,212,255,0.2)',
            borderTopColor: '#E8732A', borderRadius: '50%',
            animation: 'spin 0.8s linear infinite',
          }} />
        </div>
      )}
      {status === 'error' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
          flexDirection: 'column', gap: 8, padding: 20, textAlign: 'center',
        }}>
          <span style={{ fontSize: 28, opacity: 0.6 }}>📡</span>
          <div style={{ fontSize: 11, fontWeight: 600, color: '#E89B2A' }}>
            Site Connection Lost
          </div>
          <div style={{ fontSize: 10, color: '#4A5268', lineHeight: 1.5, maxWidth: 240 }}>
            Evidence is safely bookmarked on the local NVR and will be viewable when connection is restored.
          </div>
          <button
            onClick={() => { if (src) { setStatus('loading'); /* triggers re-render with useEffect */ } }}
            style={{
              marginTop: 4, padding: '5px 14px', borderRadius: 4, fontSize: 10, fontWeight: 600,
              background: 'rgba(0,212,255,0.08)', border: '1px solid rgba(0,212,255,0.2)',
              color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ↻ Retry Connection
          </button>
        </div>
      )}
    </div>
  );
}
