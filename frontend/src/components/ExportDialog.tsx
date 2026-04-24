'use client';

import { useState, useEffect } from 'react';
import { Camera, ExportJob, createExport, listExports } from '@/lib/api';

interface ExportDialogProps {
    cameras: Camera[];
    onClose: () => void;
}

export default function ExportDialog({ cameras, onClose }: ExportDialogProps) {
    const [selectedCamera, setSelectedCamera] = useState(cameras[0]?.id || '');
    const [startTime, setStartTime] = useState('');
    const [endTime, setEndTime] = useState('');
    const [exports, setExports] = useState<ExportJob[]>([]);
    const [exporting, setExporting] = useState(false);
    const [isMounted, setIsMounted] = useState(false);

    // Set default times (last hour)
    useEffect(() => {
        setIsMounted(true);
        const now = new Date();
        const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);

        setEndTime(formatDateTimeLocal(now));
        setStartTime(formatDateTimeLocal(oneHourAgo));

        // Load existing exports
        loadExports();
    }, []);

    const loadExports = async () => {
        try {
            const data = await listExports();
            setExports(data);
        } catch (err) {
            console.error('Failed to load exports:', err);
        }
    };

    const handleExport = async () => {
        if (!selectedCamera || !startTime || !endTime) return;

        setExporting(true);
        try {
            await createExport({
                camera_id: selectedCamera,
                start_time: new Date(startTime).toISOString(),
                end_time: new Date(endTime).toISOString(),
            });
            await loadExports();
        } catch (err) {
            console.error('Export failed:', err);
        }
        setExporting(false);
    };

    const formatDateTimeLocal = (date: Date) => {
        const offset = date.getTimezoneOffset();
        const local = new Date(date.getTime() - offset * 60 * 1000);
        return local.toISOString().slice(0, 16);
    };

    const formatFileSize = (bytes: number) => {
        if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
        return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    };

    const getCameraName = (id: string) => {
        return cameras.find((c) => c.id === id)?.name || id.slice(0, 8);
    };

    return (
        <div className="modal-overlay" onClick={onClose}>
            <div className="modal" style={{ maxWidth: 560 }} onClick={(e) => e.stopPropagation()}>
                <div className="modal-title">Export Video</div>

                <div className="form-group">
                    <label className="form-label">Camera</label>
                    <select
                        className="form-input"
                        value={selectedCamera}
                        onChange={(e) => setSelectedCamera(e.target.value)}
                    >
                        {cameras.map((cam) => (
                            <option key={cam.id} value={cam.id}>{cam.name}</option>
                        ))}
                    </select>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                    <div className="form-group">
                        <label className="form-label">Start Time</label>
                        <input
                            className="form-input"
                            type="datetime-local"
                            value={startTime}
                            onChange={(e) => setStartTime(e.target.value)}
                        />
                    </div>
                    <div className="form-group">
                        <label className="form-label">End Time</label>
                        <input
                            className="form-input"
                            type="datetime-local"
                            value={endTime}
                            onChange={(e) => setEndTime(e.target.value)}
                        />
                    </div>
                </div>

                <button
                    className="btn btn-primary"
                    style={{ width: '100%', marginTop: 4 }}
                    onClick={handleExport}
                    disabled={exporting}
                >
                    {exporting ? '⏳ Exporting...' : '📥 Export to MP4'}
                </button>

                {/* Recent Exports */}
                {exports.length > 0 && (
                    <div style={{ marginTop: 24 }}>
                        <h4 style={{ fontSize: 13, fontWeight: 600, marginBottom: 8, color: 'var(--text-secondary)' }}>
                            Recent Exports
                        </h4>
                        {exports.slice(0, 5).map((exp) => (
                            <div key={exp.id} className="event-item" style={{ borderRadius: 'var(--radius-sm)' }}>
                                <div className="event-info">
                                    <div className="event-info-header">
                                        <span className="event-info-type">{getCameraName(exp.camera_id)}</span>
                                        <span
                                            className="camera-card-status"
                                            style={{
                                                background:
                                                    exp.status === 'completed'
                                                        ? 'rgba(16,185,129,0.15)'
                                                        : exp.status === 'failed'
                                                            ? 'rgba(239,68,68,0.15)'
                                                            : 'rgba(245,158,11,0.15)',
                                                color:
                                                    exp.status === 'completed'
                                                        ? 'var(--accent-green)'
                                                        : exp.status === 'failed'
                                                            ? 'var(--accent-red)'
                                                            : 'var(--accent-amber)',
                                            }}
                                        >
                                            {exp.status}
                                        </span>
                                    </div>
                                    <div className="event-info-detail">
                                        {isMounted ? `${new Date(exp.start_time).toLocaleTimeString()} — ${new Date(exp.end_time).toLocaleTimeString()}` : ''}
                                        {exp.file_size > 0 && ` • ${formatFileSize(exp.file_size)}`}
                                    </div>
                                    {exp.status === 'completed' && exp.file_path && (
                                        <a
                                            href={`/exports/${exp.file_path.split('/').pop()}`}
                                            download
                                            style={{
                                                fontSize: 11,
                                                color: 'var(--accent-blue)',
                                                textDecoration: 'none',
                                                marginTop: 4,
                                                display: 'inline-block',
                                            }}
                                        >
                                            📥 Download
                                        </a>
                                    )}
                                    {exp.error && (
                                        <div style={{ fontSize: 11, color: 'var(--accent-red)', marginTop: 4 }}>
                                            {exp.error}
                                        </div>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>
                )}

                <div className="modal-actions">
                    <button className="btn" onClick={onClose}>Close</button>
                </div>
            </div>
        </div>
    );
}
