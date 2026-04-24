'use client';

import { useState, useEffect } from 'react';
import { useCreateSite, useUpdateSite, useSite } from '@/hooks/useSites';
import { useCompanies } from '@/hooks/useCustomers';

interface Props {
  onClose: () => void;
  editSiteId: string | null;
  embedded?: boolean;
}

export default function CreateSiteModal({ onClose, editSiteId, embedded }: Props) {
  const [name, setName] = useState('');
  const [street, setStreet] = useState('');
  const [city, setCity] = useState('');
  const [state, setState] = useState('');
  const [zip, setZip] = useState('');
  const [companyId, setCompanyId] = useState('');
  const [latitude, setLatitude] = useState('');
  const [longitude, setLongitude] = useState('');
  const [featureMode, setFeatureMode] = useState<'security_only' | 'security_and_safety'>('security_and_safety');

  // Risk profile
  const [riskTier, setRiskTier] = useState('medium');
  const [industry, setIndustry] = useState('');
  const [hazards, setHazards] = useState('');

  // Site contact (emergency)
  const [contactName, setContactName] = useState('');
  const [contactPhone, setContactPhone] = useState('');
  const [contactEmail, setContactEmail] = useState('');

  // Escalation policy
  const [slaAckMinutes, setSlaAckMinutes] = useState('5');
  const [slaResolveMinutes, setSlaResolveMinutes] = useState('30');
  const [escalationContact, setEscalationContact] = useState('');
  const [escalationPhone, setEscalationPhone] = useState('');

  const { data: companies = [] } = useCompanies();
  const createSite = useCreateSite();
  const updateSite = useUpdateSite();
  const { data: existingSite } = useSite(editSiteId);

  const isEditing = !!editSiteId;

  // Pre-populate form when editing an existing site
  useEffect(() => {
    if (existingSite && isEditing) {
      setName(existingSite.name ?? '');
      setStreet((existingSite as any).address ?? '');
      setCompanyId((existingSite as any).company_id ?? existingSite.company_id ?? '');
      if ((existingSite as any).latitude) setLatitude(String((existingSite as any).latitude));
      if ((existingSite as any).longitude) setLongitude(String((existingSite as any).longitude));
      setFeatureMode((existingSite.feature_mode as any) ?? 'security_and_safety');
      // Risk profile
      if ((existingSite as any).risk_tier) setRiskTier((existingSite as any).risk_tier);
      if ((existingSite as any).industry) setIndustry((existingSite as any).industry);
      if ((existingSite as any).hazards) setHazards((existingSite as any).hazards);
      // Site contact
      const sc = (existingSite as any).site_contact;
      if (sc) { setContactName(sc.name || ''); setContactPhone(sc.phone || ''); setContactEmail(sc.email || ''); }
      // Escalation
      const ep = (existingSite as any).escalation_policy;
      if (ep) {
        setSlaAckMinutes(String(ep.sla_ack_minutes || 5));
        setSlaResolveMinutes(String(ep.sla_resolve_minutes || 30));
        setEscalationContact(ep.escalation_contact || '');
        setEscalationPhone(ep.escalation_phone || '');
      }
    }
  }, [existingSite, isEditing]);

  const fullAddress = [street, city, state, zip].filter(Boolean).join(', ');
  const isPending = createSite.isPending || updateSite.isPending;
  const isError = createSite.isError || updateSite.isError;
  const errorMessage = (createSite.error as Error)?.message ?? (updateSite.error as Error)?.message;

  const handleSubmit = async () => {
    if (!name.trim() || !companyId) return;
    const payload: any = {
      name: name.trim(),
      address: fullAddress,
      company_id: companyId,
      latitude: latitude ? parseFloat(latitude) : undefined,
      longitude: longitude ? parseFloat(longitude) : undefined,
      feature_mode: featureMode,
      risk_tier: riskTier,
      industry: industry || undefined,
      hazards: hazards.trim() || undefined,
      site_contact: (contactName || contactPhone || contactEmail) ? {
        name: contactName.trim(),
        phone: contactPhone.trim(),
        email: contactEmail.trim(),
      } : undefined,
      escalation_policy: {
        sla_ack_minutes: parseInt(slaAckMinutes) || 5,
        sla_resolve_minutes: parseInt(slaResolveMinutes) || 30,
        escalation_contact: escalationContact.trim() || undefined,
        escalation_phone: escalationPhone.trim() || undefined,
      },
    };
    try {
      if (isEditing) {
        await updateSite.mutateAsync({ id: editSiteId!, data: payload });
      } else {
        await createSite.mutateAsync(payload);
      }
      onClose();
    } catch {
      // Error displayed below
    }
  };

  const bodyContent = (
        <div>
          <div className="admin-field">
            <label className="admin-label">Company *</label>
            <select
              className="admin-input"
              value={companyId}
              onChange={e => setCompanyId(e.target.value)}
              style={{ cursor: 'pointer' }}
            >
              <option value="">-- Select company --</option>
              {companies.map(c => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </div>

          <div className="admin-field">
            <label className="admin-label">Site Name *</label>
            <input
              className="admin-input"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="e.g. Southgate Power Station"
              autoFocus
            />
          </div>

          <div className="admin-field">
            <label className="admin-label">Street Address</label>
            <input
              className="admin-input"
              value={street}
              onChange={e => setStreet(e.target.value)}
              placeholder="e.g. 4200 Industrial Blvd"
            />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 80px 100px', gap: 8 }}>
            <div className="admin-field">
              <label className="admin-label">City</label>
              <input
                className="admin-input"
                value={city}
                onChange={e => setCity(e.target.value)}
                placeholder="Houston"
              />
            </div>
            <div className="admin-field">
              <label className="admin-label">State</label>
              <input
                className="admin-input"
                value={state}
                onChange={e => setState(e.target.value)}
                placeholder="TX"
                maxLength={2}
                style={{ textTransform: 'uppercase' }}
              />
            </div>
            <div className="admin-field">
              <label className="admin-label">Zip Code</label>
              <input
                className="admin-input"
                value={zip}
                onChange={e => setZip(e.target.value)}
                placeholder="77001"
                maxLength={10}
              />
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div className="admin-field">
              <label className="admin-label">Latitude</label>
              <input
                className="admin-input"
                value={latitude}
                onChange={e => setLatitude(e.target.value)}
                placeholder="29.7604"
                type="number"
                step="any"
              />
            </div>
            <div className="admin-field">
              <label className="admin-label">Longitude</label>
              <input
                className="admin-input"
                value={longitude}
                onChange={e => setLongitude(e.target.value)}
                placeholder="-95.3698"
                type="number"
                step="any"
              />
            </div>
          </div>

          <div className="admin-field">
            <label className="admin-label">Monitoring Tier *</label>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginTop: 4 }}>
              {([
                { value: 'security_only', label: '🔒 Security Only', sub: 'Cameras · Recordings · SOC events · Security reports' },
                { value: 'security_and_safety', label: '🛡️ Security + Safety', sub: 'All of the above + PPE · OSHA · vLM safety engine' },
              ] as const).map(opt => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setFeatureMode(opt.value)}
                  style={{
                    padding: '10px 12px', borderRadius: 6, textAlign: 'left', cursor: 'pointer',
                    border: featureMode === opt.value
                      ? '1px solid rgba(168,85,247,0.6)'
                      : '1px solid rgba(255,255,255,0.08)',
                    background: featureMode === opt.value
                      ? 'rgba(168,85,247,0.1)'
                      : 'rgba(255,255,255,0.02)',
                    transition: 'all 0.15s',
                  }}
                >
                  <div style={{ fontSize: 11, fontWeight: 600, color: featureMode === opt.value ? '#a855f7' : '#8891A5', marginBottom: 3 }}>
                    {opt.label}
                  </div>
                  <div style={{ fontSize: 9, color: '#4A5268', lineHeight: 1.4 }}>{opt.sub}</div>
                </button>
              ))}
            </div>
          </div>

          {/* ── Risk Profile ── */}
          <div style={{ borderTop: '1px solid rgba(255,255,255,0.06)', marginTop: 14, paddingTop: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 8 }}>Risk Profile</div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <div className="admin-field">
              <label className="admin-label">Risk Tier</label>
              <select className="admin-input" value={riskTier} onChange={e => setRiskTier(e.target.value)} style={{ cursor: 'pointer' }}>
                <option value="low">Low — Standard monitoring</option>
                <option value="medium">Medium — Elevated attention</option>
                <option value="high">High — Priority response</option>
                <option value="critical">Critical — Infrastructure / High-value</option>
              </select>
            </div>
            <div className="admin-field">
              <label className="admin-label">Industry</label>
              <select className="admin-input" value={industry} onChange={e => setIndustry(e.target.value)} style={{ cursor: 'pointer' }}>
                <option value="">-- Select --</option>
                <option value="construction">Construction</option>
                <option value="healthcare">Healthcare</option>
                <option value="datacenter">Data Center</option>
                <option value="energy">Energy / Utilities</option>
                <option value="logistics">Logistics / Warehouse</option>
                <option value="retail">Retail</option>
                <option value="education">Education</option>
                <option value="government">Government</option>
                <option value="manufacturing">Manufacturing</option>
                <option value="other">Other</option>
              </select>
            </div>
          </div>

          <div className="admin-field">
            <label className="admin-label">Hazards & Special Instructions</label>
            <textarea
              className="admin-input"
              value={hazards}
              onChange={e => setHazards(e.target.value)}
              placeholder="e.g. Guard dog on premises · High-value copper inventory · Active construction zone — hard hat required"
              rows={2}
              style={{ resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>

          {/* ── Site Emergency Contact ── */}
          <div style={{ borderTop: '1px solid rgba(255,255,255,0.06)', marginTop: 14, paddingTop: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 8 }}>Site Emergency Contact</div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 8 }}>
            <div className="admin-field">
              <label className="admin-label">Contact Name</label>
              <input className="admin-input" value={contactName} onChange={e => setContactName(e.target.value)} placeholder="John Smith" />
            </div>
            <div className="admin-field">
              <label className="admin-label">Phone</label>
              <input className="admin-input" value={contactPhone} onChange={e => setContactPhone(e.target.value)} placeholder="(555) 123-4567" type="tel" />
            </div>
            <div className="admin-field">
              <label className="admin-label">Email</label>
              <input className="admin-input" value={contactEmail} onChange={e => setContactEmail(e.target.value)} placeholder="john@site.com" type="email" />
            </div>
          </div>

          {/* ── Escalation Policy ── */}
          <div style={{ borderTop: '1px solid rgba(255,255,255,0.06)', marginTop: 14, paddingTop: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 8 }}>Escalation Policy</div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <div className="admin-field">
              <label className="admin-label">SLA — Acknowledge (minutes)</label>
              <input className="admin-input" value={slaAckMinutes} onChange={e => setSlaAckMinutes(e.target.value)} placeholder="5" type="number" min="1" />
              <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>Auto-escalate if not acknowledged within this time</div>
            </div>
            <div className="admin-field">
              <label className="admin-label">SLA — Resolution (minutes)</label>
              <input className="admin-input" value={slaResolveMinutes} onChange={e => setSlaResolveMinutes(e.target.value)} placeholder="30" type="number" min="1" />
              <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>Target time to resolve alarm after acknowledgment</div>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <div className="admin-field">
              <label className="admin-label">Escalation Contact Name</label>
              <input className="admin-input" value={escalationContact} onChange={e => setEscalationContact(e.target.value)} placeholder="Supervisor name" />
              <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>Notified when SLA is breached</div>
            </div>
            <div className="admin-field">
              <label className="admin-label">Escalation Phone</label>
              <input className="admin-input" value={escalationPhone} onChange={e => setEscalationPhone(e.target.value)} placeholder="(555) 999-0000" type="tel" />
            </div>
          </div>

          {isError && (
            <div style={{ padding: '10px 0', fontSize: 11, color: 'var(--accent-red)' }}>
              {errorMessage ?? `Failed to ${isEditing ? 'update' : 'create'} site — check the backend logs.`}
            </div>
          )}

          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, paddingTop: 12, borderTop: '1px solid rgba(255,255,255,0.06)', marginTop: 8 }}>
            <button
              className="admin-btn admin-btn-primary"
              onClick={handleSubmit}
              disabled={!name.trim() || !companyId || isPending}
            >
              {isPending ? (isEditing ? 'Saving...' : 'Creating...') : isEditing ? 'Save Changes' : 'Create Site'}
            </button>
          </div>
        </div>
  );

  if (embedded) return bodyContent;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal" onClick={e => e.stopPropagation()}>
        <div className="admin-modal-header">
          <div className="admin-modal-title">{isEditing ? 'Edit Site' : 'Create New Site'}</div>
          <button className="admin-modal-close" onClick={onClose}>x</button>
        </div>
        <div className="admin-modal-body">{bodyContent}</div>
      </div>
    </div>
  );
}
