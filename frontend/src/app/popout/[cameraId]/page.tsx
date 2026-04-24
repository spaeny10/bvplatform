'use client';

import { useParams } from 'next/navigation';
import { useState, useEffect } from 'react';
import VideoPlayer from '@/components/VideoPlayer';
import { Camera } from '@/lib/api';

export default function PopoutPage() {
    const params = useParams();
    const cameraId = params?.cameraId as string;
    const [camera, setCamera] = useState<Camera | null>(null);
    const [error, setError] = useState('');

    useEffect(() => {
        if (!cameraId) return;
        const token = localStorage.getItem('auth_token');
        fetch(`/api/cameras/${cameraId}`, {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        })
            .then(res => {
                if (!res.ok) throw new Error('Camera not found');
                return res.json();
            })
            .then(setCamera)
            .catch(err => setError(err.message));
    }, [cameraId]);

    useEffect(() => {
        if (camera) {
            document.title = `${camera.name} — Live View`;
        }
    }, [camera]);

    if (error) {
        return (
            <div style={{
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                height: '100vh', background: '#0a0f0a', color: '#888',
            }}>
                {error}
            </div>
        );
    }

    if (!camera) {
        return (
            <div style={{
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                height: '100vh', background: '#0a0f0a', color: '#888',
            }}>
                Loading...
            </div>
        );
    }

    return (
        <div style={{
            width: '100vw', height: '100vh', background: '#0a0f0a',
            display: 'flex', flexDirection: 'column',
        }}>
            {/* Minimal header */}
            <div style={{
                display: 'flex', alignItems: 'center', gap: 8,
                padding: '6px 12px', background: 'rgba(0,0,0,0.5)',
                fontSize: 13, fontWeight: 600, color: '#ccc',
                fontFamily: 'var(--font-mono, monospace)',
            }}>
                <span style={{ color: '#22c55e' }}>●</span>
                {camera.name}
                <span style={{ opacity: 0.4, fontSize: 11, marginLeft: 'auto' }}>
                    {camera.manufacturer} {camera.model} — Pop-out View
                </span>
            </div>

            {/* Full-size video */}
            <div style={{ flex: 1, position: 'relative' }}>
                <VideoPlayer
                    cameraId={camera.id}
                    cameraName={camera.name}
                    isLive={true}
                    playbackTime={new Date()}
                    selected={false}
                    hasPTZ={camera.has_ptz}
                    allowZoom={true}
                />
            </div>
        </div>
    );
}
