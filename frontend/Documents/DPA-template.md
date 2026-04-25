# Data Processing Addendum (Template)

**This is a template, not an executed agreement.** Sales and legal complete
the bracketed fields, customer signs, then the executed copy is filed with
the customer's other contract artifacts.

---

## 1. Parties

This Data Processing Addendum ("**DPA**") forms part of the Master
Services Agreement, Subscription Agreement, or other written agreement
("**Agreement**") between:

- **Service Provider:** Ironsight, [legal entity name and address] ("**Ironsight**")
- **Customer:** [customer legal name and address] ("**Customer**")

Effective Date: **[date]**

In the event of conflict between this DPA and the Agreement, this DPA
controls with respect to the processing of personal information.

---

## 2. Definitions

- **Personal Information** has the meaning given in the California
  Consumer Privacy Act of 2018, as amended by the California Privacy
  Rights Act ("**CCPA/CPRA**"), and equivalent terms under other
  applicable US state privacy laws (collectively, "**State Privacy
  Laws**").
- **Service Provider, Processor, Controller, Business, Consumer, Sale,
  Share** have the meanings given under applicable State Privacy Laws.
- **Services** means the Ironsight platform and related services as
  described in the Agreement.
- **Sub-processor** means any third party engaged by Ironsight to
  process Personal Information in connection with the Services.

---

## 3. Roles

For purposes of State Privacy Laws, Customer is the **Business** /
**Controller** and Ironsight is the **Service Provider** / **Processor**
with respect to Personal Information processed under the Agreement.

Ironsight will process Personal Information only on behalf of Customer
and only for the purposes set out in this DPA and the Agreement.

---

## 4. Permitted purposes

Ironsight will process Personal Information solely to:

(a) provide and operate the Services for Customer;
(b) generate, deliver, and retain alarm events, evidence packages, and
    operational reports for Customer;
(c) detect, prevent, and respond to security incidents on the platform;
(d) maintain audit-trail records as required by UL 827B central-station
    alarm-monitoring standards and Customer's internal policies;
(e) communicate with Customer-authorized contacts regarding events at
    Customer's sites; and
(f) perform other purposes reasonably necessary to fulfill the Agreement.

Ironsight will not:

(a) sell or share Personal Information;
(b) retain, use, or disclose Personal Information for any commercial
    purpose other than providing the Services;
(c) retain, use, or disclose Personal Information outside the direct
    business relationship with Customer; or
(d) combine Personal Information received from Customer with personal
    information received from any other source, except as permitted by
    State Privacy Laws (e.g., to detect security incidents).

---

## 5. Data categories

Categories of Personal Information processed under this DPA:

- Identifiers and contact information of Customer's authorized users
  (name, email, phone, role).
- Authentication credentials and account-security data.
- Video and image data captured by Customer's cameras at Customer's
  sites, which may include images of identifiable individuals.
- Event metadata generated from that video (detection records,
  AI-generated scene descriptions, timestamps).
- Audit-trail records of platform activity.

Ironsight does not collect biometric identifiers (face embeddings,
voiceprints, fingerprints) by default. Audio recording is disabled by
default. Either may be enabled only with Customer's express written
authorization and Customer's confirmation that all applicable
state-law consent and signage requirements have been satisfied at
Customer's sites.

---

## 6. Sub-processors

Ironsight uses Sub-processors to deliver the Services. The current
Sub-processor list is published at `/portal/subprocessors` in the
platform and incorporated by reference into this DPA.

Ironsight will:

(a) impose written obligations on each Sub-processor consistent with
    this DPA;
(b) provide Customer at least 30 days' notice of any new or replacement
    Sub-processor that processes Personal Information, except where
    emergency replacement is required for security reasons; and
(c) remain liable for the acts and omissions of its Sub-processors with
    respect to Personal Information.

If Customer reasonably objects to a new Sub-processor, the parties will
work in good faith to resolve the objection. If unresolved, Customer
may terminate the affected Service portion of the Agreement.

---

## 7. Security

Ironsight will maintain administrative, technical, and physical
safeguards designed to protect Personal Information, including:

- Encryption in transit (TLS) for all platform traffic.
- Multi-factor authentication for security-operator and administrator
  roles.
- Role-based access control with per-organization data isolation.
- Append-only audit logging of access and administrative actions.
- HMAC-signed evidence exports with chain-of-custody tracking.
- Account-lockout protections and rate-limited authentication
  endpoints.
- Vulnerability management and timely application of security
  patches.

These safeguards are described in Ironsight's UL 827B compliance
documentation, available on request.

---

## 8. Data subject requests

If Ironsight receives a request from an individual to exercise rights
under State Privacy Laws regarding Personal Information processed on
Customer's behalf, Ironsight will:

(a) without undue delay, redirect the requestor to Customer; and
(b) reasonably assist Customer in fulfilling the request, including
    by providing data export, correction, or deletion tooling within
    the platform.

Customer is responsible for verifying the identity of requestors and
for determining whether to honor a request consistent with applicable
law and Customer's exemptions or carve-outs.

---

## 9. Security incidents

Ironsight will notify Customer without undue delay, and in any event
within 72 hours, after becoming aware of a confirmed Security
Incident affecting Customer's Personal Information. The notice will
include, to the extent known:

- the nature and approximate scope of the incident;
- the categories of Personal Information involved;
- the steps Ironsight is taking to contain and remediate the incident;
  and
- a point of contact for follow-up.

"Security Incident" means an unauthorized acquisition, access, use,
disclosure, or destruction of Personal Information processed under
this DPA. Unsuccessful attempts (e.g., scans, port probes) that do not
result in a compromise are not Security Incidents.

Ironsight's incident response process is documented in `Documents/IncidentResponse.md`
and is available to Customer on request.

---

## 10. Retention and deletion

Recording retention is configured per Customer site as documented in
the Agreement or Service-configuration screens of the platform.

Audit-trail records are retained for a minimum of 365 days as required
by UL 827B central-station alarm-monitoring standards. Audit records
may not be deleted before that period elapses, even on Customer
request, in order to preserve chain-of-custody.

Closed support-ticket conversations are retained for 180 days from the
last reply, after which they are purged automatically.

On termination of the Agreement, Ironsight will, at Customer's
direction, return or delete Personal Information within 90 days,
subject to (a) the audit-retention obligation above and (b) any legal
obligation to preserve specific records.

---

## 11. Audits

Customer may audit Ironsight's compliance with this DPA once per
calendar year, on at least 30 days' written notice, during business
hours, in a manner that does not unreasonably interfere with
Ironsight's operations.

In lieu of an on-site audit, Ironsight may satisfy this obligation by
providing:

(a) Ironsight's then-current SOC 2 Type II report or equivalent
    third-party assessment, when available; and
(b) reasonable written responses to Customer's security questionnaires.

---

## 12. International transfers

The Services and all Sub-processors are located in the United States.
Ironsight will not transfer Personal Information outside the United
States without Customer's prior written consent.

---

## 13. Term

This DPA takes effect on the Effective Date and continues for the
duration of the Agreement, except that Sections 9 (Security
Incidents), 10 (Retention and Deletion), and 11 (Audits) survive
termination as needed to give them effect.

---

## 14. Signatures

| Customer | Ironsight |
|---|---|
| By: ________________________ | By: ________________________ |
| Name: ______________________ | Name: ______________________ |
| Title: _____________________ | Title: _____________________ |
| Date: ______________________ | Date: ______________________ |
