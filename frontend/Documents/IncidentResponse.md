# Incident Response Runbook

This is the operating procedure for handling a security incident
affecting Ironsight or its customers. The runbook is the canonical
reference cited in the customer DPA (§9) and the SOC 2 incident-
response policy.

**Audience:** the on-call engineer, the SOC supervisor on duty, and
the Ironsight incident commander when activated.

**Scope:** confirmed or suspected unauthorized acquisition, access,
use, disclosure, or destruction of Personal Information, recordings,
authentication credentials, or platform infrastructure.

Routine operational issues (a camera offline, a Postgres replica
behind, a Twilio quota warning) are **not** Security Incidents and
follow the operational on-call runbook, not this one.

---

## 1. Severity matrix

| Severity | Definition | Examples | Initial response time |
|---|---|---|---|
| **SEV-1 — Critical** | Confirmed compromise of customer data, credentials, or production infrastructure with active exploitation. | Recordings exfiltrated; admin credential abused; ransomware on production host; evidence-share token leaked + opened by unknown party. | < 15 minutes |
| **SEV-2 — High** | Confirmed unauthorized access without confirmed exfiltration, or active attack in progress. | Account-lockout storm with successful login; suspicious sub-processor breach affecting our keys; insider misuse confirmed. | < 30 minutes |
| **SEV-3 — Moderate** | Suspected incident, control failure, or near-miss. Investigation needed. | Audit-log discrepancy that can't be explained; vulnerability with known POC affecting our stack; lost laptop with platform access. | < 2 hours |
| **SEV-4 — Low** | Hardening / hygiene issue with no immediate compromise. | Outdated dependency with high CVE; misconfigured permission caught proactively. | Next business day |

Severity may be revised up or down as facts develop. **When in doubt,
classify higher and revise down later.**

---

## 2. Roles

| Role | Filled by | Authority |
|---|---|---|
| **Incident Commander (IC)** | First responder to acknowledge a SEV-1/2; may hand off to a more senior engineer. | Owns the timeline, declares severity changes, decides containment actions, authorizes customer notice. |
| **Scribe** | A second responder pulled in by IC for SEV-1. | Maintains the timeline, captures every command run and decision, writes the post-incident report. |
| **Communications lead** | The Ironsight executive on-call (CTO or designate). | Drafts and approves customer-facing notices; coordinates with legal counsel. |
| **SOC supervisor on duty** | Whoever is rostered. | Identifies operational impact on alarm monitoring; decides whether to fail over to manual procedures. |
| **Customer representative** | Customer-side admin named in the DPA. | Receives notice; coordinates customer-side response. |

The IC is one human at a time. If the IC needs to step away, they
**verbally hand off** to a named successor and the scribe records
the change.

---

## 3. Detection sources

In rough priority order:

1. **Audit-log anomaly alert** — failed-login storm, off-hours admin
   action, evidence-share open from unexpected geography.
2. **Engineer report** — internal team member sees something off.
3. **Customer report** — customer admin notifies us directly.
4. **Sub-processor notification** — SendGrid, Twilio, cloud host,
   vulnerability scanner.
5. **Public disclosure** — researcher report, social media, news
   coverage.

All five sources route to `security@ironsight.ai` and the on-call
pager. Customer-facing channels (`support@ironsight.ai`,
`/portal/support`) escalate to security on detection of incident
keywords.

---

## 4. Phase 1 — Detect & triage (first 15–30 minutes)

**Goal:** confirm or rule out an actual incident, set initial severity,
assign IC.

The first responder:

1. **Acknowledge the alert** in the on-call channel within 5 minutes.
2. **Assess the report.** Is this a confirmed compromise, a suspicion,
   or operational noise? If unsure, treat as SEV-3 and proceed.
3. **Set the severity** using §1 above. Record in the incident
   channel.
4. **Take the IC role** or escalate to a senior engineer if the
   responder is too junior for the severity.
5. **Open an incident channel** in the company chat tool with a
   timestamped name (e.g., `inc-2026-04-25-share-leak`).
6. **Pull in a scribe** for SEV-1/2.
7. **Notify the executive on-call** for SEV-1/2 by phone, not chat.

**Do not** start containment actions before §4 is done unless damage
is actively spreading and waiting would worsen the situation.

---

## 5. Phase 2 — Contain (next 30 minutes – 4 hours)

**Goal:** stop the bleeding without destroying forensic evidence.

Containment actions, by category:

### Credential compromise
- Force a password reset on the affected user.
- Revoke all live JWT sessions for the user (`UPDATE users SET token_version = token_version + 1`).
- Disable MFA enrollment temporarily if MFA was bypassed; force re-enrollment.
- Audit the user's recent actions in `audit_log` and capture full result set to a forensic file.

### Evidence-share token leak
- Mark token revoked in `evidence_share_tokens` (sets `revoked_at = NOW()`).
- Pull `evidence_share_opens` for that token; capture IP, user-agent,
  timestamp of every open. Save to forensic record.
- Notify the customer who issued the share within 1 hour.

### Platform-host compromise
- Isolate the affected host from the network if possible without
  losing volatile state worth preserving.
- Snapshot the disk for forensics before any destructive remediation.
- Rotate all secrets that the host held: DB credentials, JWT signing
  key, evidence HMAC key, sub-processor API keys.
- Note: rotating the evidence HMAC key invalidates prior signatures
  for new verification — that is an acceptable trade-off and is
  documented in the customer DPA.

### Sub-processor compromise (SendGrid / Twilio / cloud host)
- Confirm the scope of the sub-processor's notice.
- Rotate any keys the sub-processor held.
- Determine whether customer Personal Information was in scope.

### Suspected insider misuse
- Disable the user account.
- Preserve audit-log entries and any device-level evidence.
- Loop in the executive on-call before escalating to HR or law
  enforcement.

**Forensic preservation rule:** never `DELETE` from `audit_log`,
`evidence_share_opens`, `playback_audits`, or `deterrence_audits`.
The append-only triggers will block this anyway, but operators
should not attempt it.

---

## 6. Phase 3 — Notify (in parallel with §5 for SEV-1/2)

### Internal notice
- Executive on-call: by phone for SEV-1/2 (already done in §4).
- Engineering leadership: by chat for SEV-1/2 within 1 hour.
- Legal counsel: for any incident likely to trigger §6 customer
  notice.

### Customer notice
For incidents involving customer Personal Information, the
communications lead drafts a notice using the template in §9. Notice
goes out:

- **SEV-1 with confirmed customer data exposure:** within 24 hours
  of confirmation.
- **All confirmed Security Incidents under the DPA:** within 72
  hours of confirmation, per DPA §9.

The customer notice goes to the customer admin contact named in the
DPA, by email and confirmed by phone. Do not assume an unread email
is delivery.

### Regulatory notice
Determined case-by-case with legal counsel. State data-breach
notification statutes apply to all 50 US states; the most stringent
windows are:

- **California** — without unreasonable delay.
- **Texas, Vermont, Washington** — within 30 days.
- **Florida** — within 30 days, with risk-of-harm assessment.
- Most others — within 45–60 days.

Regulatory notice obligations are typically **the customer's** when
the customer is the controller and we are the processor. We assist;
we do not unilaterally notify regulators on the customer's behalf
without their direction.

### Public disclosure
The communications lead is the only person authorized to make public
statements about an incident. All other team members route inquiries
to that lead.

---

## 7. Phase 4 — Eradicate & recover (hours to days)

**Goal:** remove the root cause and restore normal operations with
the issue fixed.

- Patch or remove the vulnerability that enabled the incident.
- Restore systems from clean known-good state if compromised.
- Re-enable affected accounts after confirming credential hygiene.
- Verify the fix in a staging environment before production where
  feasible.
- Monitor for recurrence with elevated detection sensitivity for at
  least 30 days.

---

## 8. Phase 5 — Post-incident review (within 5 business days)

**Goal:** learn, not blame.

The IC schedules a post-incident review meeting within 5 business
days of resolution. The scribe produces a written report covering:

1. **Timeline** — every event with timestamps, from first signal to
   declared resolution.
2. **Impact** — what was affected, by how much, for how long.
3. **Root cause** — technical, process, or human-factor analysis.
   Five whys.
4. **What worked.**
5. **What didn't work.**
6. **Action items** — concrete, named-owner, dated. Filed as issues.

The report is filed at `Documents/incidents/<YYYY-MM-DD>-<slug>.md`
and indexed in `Documents/incidents/INDEX.md`. Customer-facing
incidents have a redacted external version sent to affected
customers.

The review meeting is **blameless**. The goal is to surface what
the system allowed to happen, not who pushed the button.

---

## 9. Customer notice template

```
Subject: Security incident notification — Ironsight platform

[Customer admin name],

We are writing to notify you of a security incident affecting your
Ironsight account.

What happened: [one-paragraph factual description]

When we detected it: [date / time]

What information may have been involved: [categories — be specific
about what data was and was not affected]

What we have done: [containment actions taken]

What you can do: [recommended actions for the customer]

We will continue to update you as our investigation progresses. The
next update will be on [date / time].

For follow-up, contact:
- Incident point of contact: [name, email, phone]
- General security questions: security@ironsight.ai

[Signature — incident commander or executive]
```

---

## 10. Tabletop exercises

The on-call rotation runs a tabletop exercise quarterly using one of
these scenarios:

- Stolen evidence-share token, opened by an unknown party.
- Compromised admin credential reused from a third-party breach.
- Cloud-host control-plane breach.
- SendGrid API-key leak in a public repo.
- SOC operator suspected of leaking customer footage.

Tabletop exercises do not interact with production. They produce a
report filed alongside real incident reports.

---

## 11. Document maintenance

This document is reviewed at least annually and after any SEV-1
incident. Material changes are reflected in `Documents/CHANGELOG.md`
and the customer DPA's reference to this runbook remains stable
across revisions unless the structure changes.

| Date | Change | By |
|---|---|---|
| 2026-04-25 | Initial document. | Shawn / Claude |
