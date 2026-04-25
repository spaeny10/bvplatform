# Ironsight — Security Overview

A short, customer-readable summary of how Ironsight protects data
and operations. Written for procurement and security-questionnaire
reviewers — the people who need an answer faster than reading the
full UL 827B and US-compliance documents.

For specifics, see the linked references at the end. For binding
contractual commitments, see your executed Data Processing Addendum.

---

## At a glance

| Domain | Posture |
|---|---|
| **Hosting region** | United States only |
| **Encryption in transit** | TLS for all platform traffic |
| **Encryption at rest** | Postgres + recording storage on encrypted volumes (deployment-dependent; documented per customer) |
| **Authentication** | Username + password with strong password policy; MFA (TOTP) for security-operator and administrator roles |
| **Authorization** | Role-based access control with per-organization data isolation |
| **Audit log** | Append-only, enforced at the database level by trigger; minimum 365-day retention per UL 827B |
| **Evidence integrity** | HMAC-SHA256 signatures on every evidence export with chain-of-custody open-tracking |
| **Cert targets** | UL 827B (primary); SOC 2 Type II (in progress) |
| **Sub-processors** | Limited to four US-based providers (see `/portal/subprocessors`) |
| **Biometric processing** | Disabled — face recognition / voiceprints / fingerprints are not collected |
| **Audio recording** | Disabled by default — gated behind state-specific consent flow before enable |
| **Backup posture** | Nightly logical backups with daily/weekly/monthly rotation, weekly automated restore verification |
| **Incident response** | 24h customer notice for confirmed exposure; 72h notice for any confirmed Security Incident |

---

## 1. Identity and access

### 1.1 Authentication
- Password policy: 12-character minimum, complexity requirements,
  blocked-list for common passwords.
- Account lockout after repeated failed attempts; lockout decay so
  a typo does not lock for hours.
- Rate limiting on the login endpoint (10 attempts per minute per
  client IP).
- TOTP-based multi-factor authentication required for all SOC
  operators, supervisors, and administrators.

### 1.2 Authorization
- Six roles, each scoped to specific surfaces:
  - **admin** — full platform access; rare, used for setup and
    deep operations.
  - **soc_supervisor** — global visibility, can verify alarms,
    manage operators, run reports.
  - **soc_operator** — alarm response only; no cross-tenant data
    access.
  - **site_manager** — customer-side admin for one organization.
  - **customer** — read-only views into one organization.
  - **viewer** — public-link evidence-share recipients (token-
    bearing only).
- Per-organization data isolation enforced in every API handler.
  Cross-tenant requests return 404 (not 403), so probing does not
  reveal which tenants exist.

### 1.3 Session security
- JWTs signed with a server-side secret; short-lived; revocable via
  user-token-version bumps.
- Logout is immediate and propagates across devices.

---

## 2. Data protection

### 2.1 In transit
- All platform traffic uses TLS terminated at the reverse proxy.
- Internal service-to-service communication runs on a private
  Docker network not exposed to the public internet.

### 2.2 At rest
- Postgres data volumes use the host's encrypted-storage facility
  (LUKS / BitLocker / cloud-managed disk encryption depending on
  deployment topology).
- Recording storage uses the same convention. The specific volume
  encryption mechanism is documented in the customer's deployment
  record.
- Application secrets (database password, JWT signing key, evidence
  HMAC key, third-party API keys) live in environment-loaded
  configuration, not in source.

### 2.3 Audit trail
- Every privileged action — login, configuration change, evidence
  export, alarm disposition, role change, MFA enrollment — writes
  to `audit_log`.
- The audit log is append-only at the database layer: a PL/pgSQL
  trigger blocks `UPDATE` and `DELETE`. This is enforced even
  against application-level bugs that might attempt to mutate it.
- Minimum retention is 365 days per UL 827B. Audit records are
  never automatically purged before that floor.

### 2.4 Evidence integrity
- Evidence exports (incident packages, recording clips) are signed
  with HMAC-SHA256 using a server-held key.
- Each public evidence-share token records every open: timestamp,
  IP, user-agent. The open-log is append-only; tokens are
  revocable.

---

## 3. Reliability

### 3.1 Backups
- Logical (`pg_dump`) backups taken nightly with daily / weekly /
  monthly rotation.
- Weekly automated verification: a backup is restored into a
  throwaway sandbox and a table-presence check confirms it
  round-trips.
- Off-site copy is the customer's deployment choice (S3, B2, etc.)
  and is documented per customer.
- Default RPO 24 hours, RTO 4 hours. Tighter objectives are
  available with WAL archiving or a streaming replica — see
  `Documents/DisasterRecovery.md`.

### 3.2 Disaster recovery
- The DR runbook documents in-place restore, restore onto a new
  host, and recovery from `.env` loss.
- A full DR drill is scheduled at least every six months and
  produces a written report.

### 3.3 High availability
- Worker leader election uses Postgres advisory locks so
  background jobs (notifications, monthly summaries) run exactly
  once even when multiple worker replicas are deployed.
- API replicas are stateless and run behind a load balancer in
  multi-host deployments.

---

## 4. Third parties

| Provider | Purpose | Required? | Location |
|---|---|---|---|
| SendGrid (Twilio Inc.) | Email | Yes | US |
| Twilio | SMS | Optional | US |
| Cloud infrastructure | Compute / storage | Yes | US (provider named per deployment) |
| Vision-language model (Qwen-VL) | AI scene descriptions | Optional | Self-hosted, US |

Recordings are not transmitted to third parties by the platform.
The current list is also published at `/portal/subprocessors` and
mirrors §5 of `USCompliance.md`.

---

## 5. Security operations

- **Vulnerability management.** Dependencies tracked with
  `go.sum` / `package-lock.json`. Known-CVE high-severity
  dependency findings are addressed within 30 days; critical within
  72 hours.
- **Change management.** All production changes flow through git
  pull requests with peer review before merging to `main`. Direct
  pushes to `main` are blocked at the repo level.
- **Penetration testing.** Annual external pentest engagement
  scheduled.
- **Vendor reviews.** Sub-processor security postures reviewed
  annually; material changes communicated to customer admins with
  at least 30 days' notice.
- **Personnel security.** Access to production systems is granted
  only to engineers actively on-call, by named individual; access
  reviews quarterly. Departing personnel access revoked within one
  business day.

---

## 6. Compliance posture

- **UL 827B (central-station alarm monitoring).** Primary
  certification target. The platform's audit, MFA, password
  policy, account lockout, evidence-integrity, and retention
  controls are designed to UL 827B requirements. See
  `UL827B_Compliance.md`.
- **SOC 2 Type II.** Foundations in place; auditor engagement in
  progress.
- **State privacy laws (CCPA/CPRA, VCDPA, CPA, etc.).** Ironsight
  acts as service provider / processor. Customer DPAs include the
  contractual terms required by the relevant state-law carve-outs.
- **State data-breach notification (all 50 states).** Covered by
  the incident-response runbook.
- **Biometric privacy laws (BIPA, CUBI, WA HB 1493, NY SHIELD
  biometric provisions).** Avoided by not collecting biometric
  identifiers in the default product. Enabling face recognition
  requires per-state legal review and is not available without
  explicit customer addendum.
- **Two-party-consent audio statutes** (CA, FL, IL, MD, MA, MT, NH,
  PA, WA, and others). Avoided by not recording audio in the
  default product. Enabling audio recording requires per-site
  consent / signage workflow.
- **GDPR / UK GDPR.** Out of scope. Ironsight is sold to US
  customers only.

---

## 7. Reporting a security concern

Email **`security@ironsight.ai`** with details. Include reproduction
steps if the issue is exploitable. We will acknowledge within one
business day and provide a status update within five.

For platform-customer support unrelated to security, use the
in-product support widget or email **`support@ironsight.ai`**.

---

## 8. References

- [`UL827B_Compliance.md`](./UL827B_Compliance.md) — full UL 827B
  control mapping.
- [`USCompliance.md`](./USCompliance.md) — US regulatory posture
  and feature gating.
- [`DisasterRecovery.md`](./DisasterRecovery.md) — backup and
  restore runbook with RPO/RTO.
- [`IncidentResponse.md`](./IncidentResponse.md) — incident
  handling, severity matrix, customer notice timing.
- [`DPA-template.md`](./DPA-template.md) — Data Processing
  Addendum template.
- `/portal/privacy` — customer-facing privacy notice.
- `/portal/subprocessors` — current sub-processor list.

---

## 9. Document maintenance

| Date | Change | By |
|---|---|---|
| 2026-04-25 | Initial overview. | Shawn / Claude |
