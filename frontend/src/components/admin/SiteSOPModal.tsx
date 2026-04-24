'use client';

import { useState, useEffect, useMemo } from 'react';
import { getSiteSOPs, createSiteSOP, updateSiteSOP, deleteSiteSOP } from '@/lib/ironsight-api';
import { useSites } from '@/hooks/useSites';
import type { SiteSOP } from '@/types/ironsight';

interface Props {
  siteId: string;
  onClose: () => void;
  embedded?: boolean;
}

const CATEGORY_COLORS: Record<string, { bg: string; border: string; text: string; icon: string }> = {
  emergency: { bg: 'rgba(255,51,85,0.1)', border: 'rgba(255,51,85,0.25)', text: '#EF4444', icon: '🚨' },
  safety: { bg: 'rgba(255,204,0,0.1)', border: 'rgba(255,204,0,0.25)', text: '#E89B2A', icon: '⚠️' },
  access: { bg: 'rgba(0,212,255,0.1)', border: 'rgba(0,212,255,0.25)', text: '#E8732A', icon: '🔒' },
  equipment: { bg: 'rgba(168,85,247,0.1)', border: 'rgba(168,85,247,0.25)', text: '#a855f7', icon: '🏗️' },
  general: { bg: 'rgba(255,255,255,0.04)', border: 'rgba(255,255,255,0.08)', text: '#8891A5', icon: '📋' },
};

const PRIORITY_COLORS: Record<string, string> = {
  critical: '#EF4444',
  high: '#EF4444',
  normal: '#8891A5',
};

export default function SiteSOPModal({ siteId, onClose, embedded }: Props) {
  const { data: sites = [] } = useSites();
  const site = sites.find(s => s.id === siteId);

  const [sops, setSOPs] = useState<SiteSOP[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [showAddForm, setShowAddForm] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);

  // New SOP form state
  const [newTitle, setNewTitle] = useState('');
  const [newCategory, setNewCategory] = useState<SiteSOP['category']>('safety');
  const [newPriority, setNewPriority] = useState<SiteSOP['priority']>('normal');
  const [newSteps, setNewSteps] = useState('');

  // Edit SOP form state
  const [editTitle, setEditTitle] = useState('');
  const [editCategory, setEditCategory] = useState<SiteSOP['category']>('safety');
  const [editPriority, setEditPriority] = useState<SiteSOP['priority']>('normal');
  const [editSteps, setEditSteps] = useState('');

  useEffect(() => {
    getSiteSOPs(siteId).then(data => {
      setSOPs(data);
      setLoading(false);
    });
  }, [siteId]);

  const handleAddSOP = async () => {
    if (!newTitle.trim()) return;
    const steps = newSteps.split('\n').filter(s => s.trim());
    const result = await createSiteSOP({
      site_id: siteId,
      title: newTitle.trim(),
      category: newCategory,
      priority: newPriority,
      steps,
      contacts: [],
      updated_by: 'Admin',
    });
    setSOPs(prev => [...prev, result]);
    setShowAddForm(false);
    setNewTitle('');
    setNewSteps('');
  };

  const handleDeleteSOP = async (sopId: string) => {
    await deleteSiteSOP(sopId);
    setSOPs(prev => prev.filter(s => s.id !== sopId));
  };

  const startEdit = (sop: SiteSOP) => {
    setEditingId(sop.id);
    setEditTitle(sop.title);
    setEditCategory(sop.category);
    setEditPriority(sop.priority);
    setEditSteps(sop.steps.join('\n'));
    setExpandedId(sop.id);
  };

  const handleSaveEdit = async (sopId: string) => {
    const steps = editSteps.split('\n').filter(s => s.trim());
    await updateSiteSOP(sopId, {
      title: editTitle,
      category: editCategory,
      priority: editPriority,
      steps,
      contacts: sops.find(s => s.id === sopId)?.contacts ?? [],
      updated_by: 'Admin',
    });
    setSOPs(prev => prev.map(s =>
      s.id === sopId
        ? { ...s, title: editTitle, category: editCategory, priority: editPriority, steps, updated_at: new Date().toISOString() }
        : s
    ));
    setEditingId(null);
  };

  // Group by category
  const grouped = useMemo(() => {
    const map: Record<string, SiteSOP[]> = {};
    sops.forEach(sop => {
      if (!map[sop.category]) map[sop.category] = [];
      map[sop.category].push(sop);
    });
    return map;
  }, [sops]);

  const bodyContent = (
        <div style={{ padding: 0 }}>
          {/* Inline toolbar for add */}
          <div style={{ padding: '10px 16px', borderBottom: '1px solid rgba(255,255,255,0.04)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: 11, color: '#4A5268' }}>{sops.length} procedure{sops.length !== 1 ? 's' : ''}</span>
            <button className="admin-btn admin-btn-primary" style={{ fontSize: 11, padding: '5px 12px' }} onClick={() => setShowAddForm(true)}>
              + Add SOP
            </button>
          </div>
          {loading && (
            <div style={{ padding: 40, textAlign: 'center', color: '#4A5268' }}>Loading SOPs…</div>
          )}

          {/* Add form */}
          {showAddForm && (
            <div style={{ padding: 16, borderBottom: '1px solid rgba(255,255,255,0.06)', background: 'rgba(139,92,246,0.03)' }}>
              <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 10, color: '#a78bfa' }}>New Standard Operating Procedure</div>
              <div className="admin-field" style={{ marginBottom: 10 }}>
                <input
                  className="admin-input"
                  placeholder="SOP Title (e.g. 'Fire Alarm Response')"
                  value={newTitle}
                  onChange={e => setNewTitle(e.target.value)}
                  autoFocus
                />
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 }}>
                <select className="admin-input" value={newCategory} onChange={e => setNewCategory(e.target.value as SiteSOP['category'])} style={{ cursor: 'pointer' }}>
                  <option value="emergency">🚨 Emergency</option>
                  <option value="safety">⚠️ Safety</option>
                  <option value="access">🔒 Access</option>
                  <option value="equipment">🏗️ Equipment</option>
                  <option value="general">📋 General</option>
                </select>
                <select className="admin-input" value={newPriority} onChange={e => setNewPriority(e.target.value as SiteSOP['priority'])} style={{ cursor: 'pointer' }}>
                  <option value="critical">Critical</option>
                  <option value="high">High</option>
                  <option value="normal">Normal</option>
                </select>
              </div>
              <div className="admin-field" style={{ marginBottom: 10 }}>
                <textarea
                  className="admin-input"
                  placeholder="Steps (one per line)&#10;1. First action&#10;2. Second action&#10;3. Third action"
                  value={newSteps}
                  onChange={e => setNewSteps(e.target.value)}
                  rows={5}
                  style={{ resize: 'vertical', fontFamily: 'inherit' }}
                />
              </div>
              <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                <button className="admin-btn admin-btn-ghost" onClick={() => setShowAddForm(false)}>Cancel</button>
                <button className="admin-btn admin-btn-primary" onClick={handleAddSOP} disabled={!newTitle.trim()}>Save SOP</button>
              </div>
            </div>
          )}

          {/* SOPs grouped by category */}
          {!loading && Object.entries(grouped).map(([category, categorySops]) => {
            const cc = CATEGORY_COLORS[category] || CATEGORY_COLORS.general;
            return (
              <div key={category}>
                <div style={{
                  padding: '10px 16px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5,
                  textTransform: 'uppercase' as const, color: cc.text,
                  background: cc.bg, borderBottom: `1px solid ${cc.border}`,
                  display: 'flex', alignItems: 'center', gap: 6,
                }}>
                  {cc.icon} {category} ({categorySops.length})
                </div>
                {categorySops.map(sop => {
                  const isExpanded = expandedId === sop.id;
                  return (
                    <div
                      key={sop.id}
                      style={{
                        borderBottom: '1px solid rgba(255,255,255,0.04)',
                        cursor: 'pointer',
                        transition: 'background 0.15s',
                      }}
                    >
                      {/* Header row */}
                      <div
                        onClick={() => setExpandedId(isExpanded ? null : sop.id)}
                        style={{
                          padding: '10px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                        }}
                        onMouseEnter={e => (e.currentTarget.style.background = 'rgba(139,92,246,0.04)')}
                        onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                          <span style={{ fontSize: 12, color: '#4A5268', transition: 'transform 0.15s', transform: isExpanded ? 'rotate(90deg)' : 'none' }}>▶</span>
                          <div>
                            <div style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0' }}>{sop.title}</div>
                            <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1, fontFamily: "'JetBrains Mono', monospace" }}>
                              {sop.steps.length} steps · Updated {new Date(sop.updated_at).toLocaleDateString()} by {sop.updated_by}
                            </div>
                          </div>
                        </div>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <span style={{
                            fontSize: 8, padding: '2px 6px', borderRadius: 2, fontWeight: 600,
                            letterSpacing: 0.5, textTransform: 'uppercase' as const,
                            color: PRIORITY_COLORS[sop.priority] || '#8891A5',
                            border: `1px solid ${PRIORITY_COLORS[sop.priority] || '#4A5268'}40`,
                          }}>
                            {sop.priority}
                          </span>
                          <button
                            className="admin-btn admin-btn-ghost"
                            style={{ fontSize: 9, padding: '2px 6px' }}
                            onClick={(e) => { e.stopPropagation(); startEdit(sop); }}
                          >
                            Edit
                          </button>
                          <button
                            className="admin-btn admin-btn-danger"
                            style={{ fontSize: 9, padding: '2px 6px' }}
                            onClick={(e) => { e.stopPropagation(); handleDeleteSOP(sop.id); }}
                          >
                            ✕
                          </button>
                        </div>
                      </div>

                      {/* Expanded content — edit form or read-only view */}
                      {isExpanded && editingId === sop.id && (
                        <div style={{ padding: '10px 16px 14px 38px', borderTop: '1px solid rgba(255,255,255,0.04)' }}>
                          <div className="admin-field" style={{ marginBottom: 8 }}>
                            <input
                              className="admin-input"
                              value={editTitle}
                              onChange={e => setEditTitle(e.target.value)}
                              placeholder="SOP Title"
                              autoFocus
                            />
                          </div>
                          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 8 }}>
                            <select className="admin-input" value={editCategory} onChange={e => setEditCategory(e.target.value as SiteSOP['category'])} style={{ cursor: 'pointer' }}>
                              <option value="emergency">🚨 Emergency</option>
                              <option value="safety">⚠️ Safety</option>
                              <option value="access">🔒 Access</option>
                              <option value="equipment">🏗️ Equipment</option>
                              <option value="general">📋 General</option>
                            </select>
                            <select className="admin-input" value={editPriority} onChange={e => setEditPriority(e.target.value as SiteSOP['priority'])} style={{ cursor: 'pointer' }}>
                              <option value="critical">Critical</option>
                              <option value="high">High</option>
                              <option value="normal">Normal</option>
                            </select>
                          </div>
                          <div className="admin-field" style={{ marginBottom: 8 }}>
                            <textarea
                              className="admin-input"
                              value={editSteps}
                              onChange={e => setEditSteps(e.target.value)}
                              placeholder="Steps (one per line)"
                              rows={5}
                              style={{ resize: 'vertical', fontFamily: 'inherit' }}
                            />
                          </div>
                          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                            <button className="admin-btn admin-btn-ghost" style={{ fontSize: 10 }} onClick={() => setEditingId(null)}>Cancel</button>
                            <button className="admin-btn admin-btn-primary" style={{ fontSize: 10 }} onClick={() => handleSaveEdit(sop.id)} disabled={!editTitle.trim()}>Save</button>
                          </div>
                        </div>
                      )}

                      {isExpanded && editingId !== sop.id && (
                        <div style={{ padding: '0 16px 14px 38px' }}>
                          <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: '#4A5268', marginBottom: 6 }}>
                            PROCEDURE STEPS
                          </div>
                          <ol style={{ margin: 0, paddingLeft: 16, fontSize: 12, color: '#8891A5', lineHeight: 1.8 }}>
                            {sop.steps.map((step, i) => (
                              <li key={i} style={{ marginBottom: 2 }}>{step}</li>
                            ))}
                          </ol>
                          {sop.contacts.length > 0 && (
                            <>
                              <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: '#4A5268', marginTop: 12, marginBottom: 6 }}>
                                CONTACTS
                              </div>
                              <div style={{ display: 'flex', flexWrap: 'wrap' as const, gap: 8 }}>
                                {sop.contacts.map((c, i) => (
                                  <div key={i} style={{
                                    padding: '6px 10px', borderRadius: 4, fontSize: 11,
                                    background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.06)',
                                  }}>
                                    <div style={{ fontWeight: 600, color: '#E4E8F0' }}>{c.name}</div>
                                    <div style={{ fontSize: 9, color: '#4A5268' }}>
                                      {c.role}{c.phone ? ` · ${c.phone}` : ''}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            </>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            );
          })}

          {!loading && sops.length === 0 && !showAddForm && (
            <div style={{ padding: 40, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
              No SOPs defined for this site yet. Click &ldquo;+ Add SOP&rdquo; to create one.
            </div>
          )}
        </div>
  );

  if (embedded) return bodyContent;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal wide" onClick={e => e.stopPropagation()} style={{ width: 720, maxHeight: '85vh' }}>
        <div className="admin-modal-header">
          <div>
            <div className="admin-modal-title">Site SOPs</div>
            <div style={{ fontSize: 11, color: '#4A5268', marginTop: 2 }}>
              {site?.name || siteId} · {sops.length} procedure{sops.length !== 1 ? 's' : ''}
            </div>
          </div>
          <button className="admin-modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="admin-modal-body" style={{ padding: 0 }}>{bodyContent}</div>
        <div className="admin-modal-footer">
          <button className="admin-btn admin-btn-ghost" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}
