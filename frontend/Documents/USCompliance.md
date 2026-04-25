# US Compliance Posture — BV-Platform / Ironsight

**Market scope:** United States only. No EU customers. GDPR is explicitly **out of scope**.

This document is the canonical reference for which compliance regimes apply to
Ironsight, which features are gated behind state-specific legal review, and what
customer-facing artifacts we ship to satisfy US B2B procurement.

If a future scope change adds EU customers, treat this document as **invalid
until rewritten** — many of the choices below assume US-only data subjects.

---

## 1. Regulatory regimes

| Regime | Applies? | Status | Notes |
|---|---|---|---|
| **UL 827B** (Central-station alarm-monitoring) | ✅ Primary cert target | In progress | See `UL827B_Compliance.md`. Drives the audit log, MFA, password policy, account lockout, evidence integrity, and recording-retention design. |
| **SOC 2 Type II** | ✅ Expected by B2B procurement | Foundations in place | Technical controls (RBAC, MFA, audit log, encryption, lockout, change management via git) cover Security TSC. Need formal policies + auditor engagement. |
| **CCPA / CPRA** (California) | ⚠️ Conditional | B2B "service provider" carve-out applies | Triggers if any customer has California consumer data subjects. Operational obligation: respect customer DSARs (we honor them on customer's behalf via DPA), don't sell/share data, support deletion. |
| **State comprehensive privacy laws** (VA, CO, CT, UT, TX, OR, MT, IA, DE, NJ, NH, KY, MD, MN, RI, IN, TN) | ⚠️ B2B carve-outs apply | Same posture as CCPA | All have B2B / service-provider exemptions when a written contract exists. The DPA template is the gating artifact. |
| **State data-breach notification laws** (all 50 states) | ✅ Universal | Documented in `IncidentResponse.md` | Most stringent: California ("without unreasonable delay"). Some require notification within 30–45 days. |
| **BIPA** (Illinois Biometric Information Privacy Act) | 🔴 Gated | **Disabled** | Strict liability, $1k–$5k per violation, broad private right of action. Triggers if we store biometric identifiers (face geometry, voiceprints, fingerprints). See §3 below. |
| **CUBI** (Texas Capture or Use of Biometric Identifier) | 🔴 Gated | **Disabled** | Civil penalty up to $25k per violation. Same trigger as BIPA. |
| **WA HB 1493 / NY SHIELD biometric provisions** | 🔴 Gated | **Disabled** | Same trigger. |
| **State two-party consent / wiretap laws** (CA, FL, IL, MD, MA, MT, NH, PA, WA, and others) | 🔴 Gated | **Disabled** | Triggers if we capture audio. Cameras today are video-only. See §3 below. |
| **HIPAA** | Conditional | Not enabled | Triggers only if a customer monitors a healthcare facility where PHI may be in-frame. If so, customer signs a Business Associate Agreement. |
| **PCI-DSS** | ❌ Not applicable | We don't process payment cards. | If a billing system is added later, route payments through Stripe / a tokenizing processor — never store PANs. |
| **CJIS** (Criminal Justice Info Services) | Conditional | Not enabled | Triggers only if a law-enforcement customer integrates. Has its own access-control + audit requirements; we'd need a separate review. |
| **GDPR** | ❌ Out of scope | — | Saved for the record: we don't sell to EU customers, so DSAR plumbing, RoPA, sub-processor disclosures on a GDPR basis, EU SCCs, Art 22 transparency, and DPIA on a GDPR basis are **not** built and **not** planned. |

---

## 2. What's already done (control inventory)

These controls were shipped as part of UL 827B and the platform-hardening
phases. They also satisfy the bulk of what US procurement asks for under
SOC 2 / CCPA service-provider obligations.

| Control | Where | Compliance value |
|---|---|---|
| Append-only audit log with PL/pgSQL trigger | `audit_log` table | UL 827B, SOC 2 (CC7.1), state breach laws (forensics) |
| MFA (TOTP) for SOC roles | `internal/auth/mfa.go` | UL 827B, SOC 2 (CC6.1) |
| Strong password policy + account lockout | `internal/auth/password.go` | UL 827B, SOC 2 |
| Per-org RBAC, cross-tenant 404 | `internal/api/authz.go` | SOC 2 (CC6.3), CCPA (purpose limitation) |
| HMAC-SHA256 evidence signing | `internal/evidence/signing.go` | UL 827B (chain of custody), state breach laws (admissibility) |
| Configurable retention for recordings + segments | `internal/recording/retention.go` | UL 827B, state data-minimization expectations |
| TMA-AVS-01 alarm validation scoring | `internal/avs/scoring.go` | UL 827B |
| Append-only `playback_audits` and `deterrence_audits` | DB | UL 827B |
| Encryption-in-transit (TLS at proxy) | Deployment | SOC 2 (CC6.6), state breach laws |
| Rate-limited auth endpoint | `RateLimitLogin` middleware | UL 827B (brute-force throttle) |

---

## 3. Hard-line gated features

These features are **technically possible** in the codebase but **must not be
turned on** without explicit per-state legal review and consent flow.

### 3.1 Face recognition / biometric matching — 🔴 GATED

**Why gated:** BIPA (IL), CUBI (TX), and a growing list of state biometric
statutes impose strict-liability penalties for collecting or storing biometric
identifiers without written consent.

**What is OK today:**
- The VLM (Qwen vision-language model) produces *natural-language descriptions*
  of frames ("a person in a red jacket near the gate"). That is descriptive
  metadata, not a biometric identifier.
- Standard person/vehicle/animal detection bounding boxes via the detector.

**What would trip the gate:**
- Storing face embeddings or geometry vectors keyed to identity.
- "Recognize this person across cameras / sites" features.
- Watchlist matching against face galleries.

**If you want to enable face-rec in the future:**
1. Per-state legal review (start with IL, TX, WA, NY).
2. Customer opt-in via signed addendum naming the biometric data category.
3. Subject-consent capture flow for any tracked individual (employees, etc.).
4. Retention policy — BIPA mandates destruction within 3 years of last
   interaction or once purpose is served.
5. Public-facing biometric privacy policy.
6. Update this document and `UL827B_Compliance.md`.

### 3.2 Audio capture — 🔴 GATED

**Why gated:** Eleven-plus US states are two-party consent ("all-party
consent") jurisdictions for audio recording. Recording audio without consent
is a criminal offense in several of them.

**What is OK today:**
- Two-way audio for live operator-to-site talkdown (real-time, no storage).
- The audio API endpoints exist (`internal/api/audio.go`,
  `internal/onvif/backchannel.go`) but recording-to-disk is off by default.

**What would trip the gate:**
- Persisting audio tracks alongside video segments.
- Triggering audio capture as part of an alarm response without explicit
  customer-side consent flow (signage + on-camera audible notice).

**If you want to enable audio recording in the future:**
1. Per-state matrix of consent rules.
2. Per-site signage + audible disclosure verification.
3. Customer addendum acknowledging two-party-consent obligations.
4. Camera-level toggle that defaults to OFF and is auditable.
5. Update this document and `UL827B_Compliance.md`.

### 3.3 Cross-site identity correlation — 🟡 SOFT GATE

**Why soft-gated:** Even without face-rec, persistent re-identification of
individuals across sites approaches state biometric and consumer-privacy
concerns.

**What is OK today:**
- Per-incident correlation within a single site by event metadata.

**What would trip the gate:**
- Building a cross-tenant "person ID" graph.
- Sharing identity-linked metadata across customer organizations.

---

## 4. Customer-facing artifacts (procurement-ready)

| Artifact | Status | Path / link |
|---|---|---|
| Privacy notice (US-generic) | ✅ Shipped | `frontend/src/app/portal/privacy/page.tsx` |
| Sub-processor list (informational) | ✅ Shipped | `frontend/src/app/portal/subprocessors/page.tsx` |
| Data Processing Addendum (DPA) template | ✅ Shipped | `frontend/Documents/DPA-template.md` |
| Incident-response runbook | ✅ Shipped | `frontend/Documents/IncidentResponse.md` |
| UL 827B compliance summary | ✅ Shipped | `frontend/Documents/UL827B_Compliance.md` |
| Customer-facing security overview | ⏳ Planned | `frontend/Documents/SecurityOverview.md` |

The remaining `⏳` item is tracked in the next compliance milestone.

---

## 5. Sub-processors (current)

These are external services that may process customer data on our behalf. All
are US-based; none transfer data outside the US under our deployment model.

| Sub-processor | Purpose | Data categories | US-only? |
|---|---|---|---|
| **SendGrid** (Twilio Inc.) | Outbound email (alarm notifications, evidence shares, support tickets) | Recipient email, alarm metadata, ticket subject/body | ✅ |
| **Twilio** | Outbound SMS (alarm notifications) | Recipient phone, short alarm summary | ✅ |
| **Cloud host** (TBD per deployment) | Compute, storage, networking | All platform data | ✅ (selectable per-deployment) |
| **VLM provider** (self-hosted Qwen by default) | Vision-language enrichment of alarm frames | Frame thumbnails, derived text descriptions | ✅ (self-hosted) |

**Notes:**
- Recordings are **never** sent to third parties by the platform itself.
  Customers can export evidence shares, but the share-target is the customer's
  choice.
- The VLM is self-hosted in the default deployment topology. If a future
  deployment uses a managed LLM API, this table must be updated and the
  customer notified per their DPA.

---

## 6. Retention policy summary

Configurable retention is in place for recordings and the support-ticket
inbox. Audit-trail tables are explicitly **not** auto-purged — they are the
chain-of-custody record and are governed by UL 827B's 365-day minimum.

| Data | Default | Configurable per | Worker | Notes |
|---|---|---|---|---|
| Camera recordings (`segments`) | 30 days | Site, storage location | `internal/recording/retention.go` | Disk-cap enforced first, then per-site/storage retention |
| Recording exports | Same as parent recording | — | Same worker | |
| `audit_log` | **365-day minimum, never auto-purged** | — | n/a | UL 827B requires retention; PL/pgSQL trigger blocks DELETE |
| `playback_audits`, `deterrence_audits` | Never auto-purged | — | n/a | Append-only by trigger |
| `evidence_share_opens` | Never auto-purged | — | n/a | Forensic record |
| `support_tickets` + `support_messages` | 180 days after close | System default | `internal/recording/retention.go` (extended) | Purge applies only to `status='closed'` tickets older than the cutoff |
| Resolved notifications | 90 days | System default | Same worker | |

If a customer requests deletion of specific records (rare in B2B; usually
covered by the contract end-of-term), it is performed as a **manual
operation in a documented signed-maintenance window**, never via automated
retention purge.

---

## 7. What this document does NOT do

- It is not legal advice. Specific deployments may require attorney review,
  especially when expanding to new states or vertical markets (healthcare,
  K-12 education, public-sector).
- It does not replace customer-side obligations. Customers are typically the
  data controller for the surveillance footage we host on their behalf; their
  obligations to *their* data subjects (employees, visitors, contractors) are
  covered in the customer's signage, employee handbook, and DPA with us.
- It does not cover deployment-specific compliance (HIPAA BAA terms, CJIS
  network isolation, FedRAMP) — those are addressed at the customer
  engagement level.

---

## 8. Change history

| Date | Change | By |
|---|---|---|
| 2026-04-25 | Initial document created. US-only scope confirmed. GDPR explicitly out of scope. | Shawn / Claude |
