# 06 — Portal & Platform

The multi-tenant layer on top of the NVR: companies (customer orgs), sites,
the customer portal (dashboard, incidents, history, contacts), support
tickets, evidence share links, the public status page, and the feature-flag
service that hides parked surfaces at MVP. Customer-facing portal basics are
core; the SOC-supervisor share/ticket consoles are parked behind flags.

## Companies management {#companies-management}

| Field | Value |
|---|---|
| **ID** | `companies-management` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Admin CRUD for customer organizations and their portal users, from the admin Sites & Customers tab. Companies own sites; company users get portal logins. |
| **Frontend** | `frontend/src/components/admin/SitesAndCustomersTab.tsx` · `frontend/src/components/admin/CompanyCard.tsx` · `frontend/src/components/admin/CustomerAccessModal.tsx` · `frontend/src/hooks/useCustomers.ts` |
| **Routes** | `GET /api/v1/companies` · `POST /api/v1/companies` · `PUT /api/v1/companies/{id}` · `DELETE /api/v1/companies/{id}` · `GET /api/v1/companies/{companyId}/users` · `POST /api/v1/companies/{companyId}/users` · `DELETE /api/v1/companies/{companyId}/users/{userId}` |
| **Tables** | organizations, company_users |
| **Flag** | — |
| **Docs** | [data-model.md](../data-model.md) |
| **Smoke test** | Admin → Sites & Customers → + Add Company → create. Switch to companies view; new card appears with 0 sites. |
| **Notes** | Cannot be `working`: `getCompany` (ironsight-api.ts:185) calls `GET /api/v1/companies/{id}` which has no backend route (api-coverage Table C, 404) — harmless today only because the `useCompany` hook has no component callers. Inverse gap too: `PUT`/`DELETE` company and `DELETE` company user exist in the backend with no UI (Table B) — there is no edit/delete-company button anywhere. The `POST /api/v1/companies/{companyId}/users` create route exists server-side, but its zero-caller `createCompanyUser` client was deleted in the 2026-06 dead-code cleanup (Table B now); per-site user assignment is broken, see [[site-assignments]]. |

## Sites CRUD + site detail {#sites-crud}

| Field | Value |
|---|---|
| **ID** | `sites-crud` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Create/edit/delete monitored sites (admin) and the customer-facing site drill-down page showing cameras, recent incidents, security events with snapshots, and alarm snooze. |
| **Frontend** | `frontend/src/hooks/useSites.ts` · `frontend/src/components/admin/CreateSiteModal.tsx` · `frontend/src/components/admin/SiteConfigModal.tsx` · `frontend/src/app/portal/sites/[id]/page.tsx` |
| **Routes** | `GET /api/v1/sites` · `POST /api/v1/sites` · `GET /api/v1/sites/{id}` · `PUT /api/v1/sites/{id}` · `DELETE /api/v1/sites/{id}` · `GET /api/v1/sites/{siteId}/cameras` |
| **Tables** | sites |
| **Flag** | — |
| **Docs** | [data-model.md](../data-model.md) · [media-auth.md](../media-auth.md) |
| **Smoke test** | Admin → Sites & Customers → + Create Site. Open /portal/sites/&lt;id&gt;; rename via config modal; reload and confirm the rename stuck. |
| **Notes** | All listed routes verified wired with real callers; list/detail RBAC-scoped server-side (org match or `assigned_site_ids`). Site snooze is stored inside the site JSON via `PUT /api/v1/sites/{id}`. Drill-down event snapshots use signed media-mint URLs. One dead limb in `useSites.ts`: `useSiteCompliance` calls `GET /api/v1/sites/{*}/compliance` (Table C, 404) but has no component callers — belongs to parked compliance (area 08). Monitoring-schedule writes are deliberately a separate endpoint, see [[monitoring-schedule]]. |

## Monitoring schedule {#monitoring-schedule}

| Field | Value |
|---|---|
| **ID** | `monitoring-schedule` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Per-site weekly monitoring windows ({day, start, end, enabled}) that control when the SOC actively watches a site. Edited in the admin site-config Schedule tab. |
| **Frontend** | `frontend/src/components/admin/SiteScheduleModal.tsx` · `frontend/src/components/admin/ScheduleWindowsEditor.tsx` · `frontend/src/hooks/useSites.ts` |
| **Routes** | `PUT /api/v1/sites/{id}/monitoring-schedule` |
| **Tables** | sites |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Admin → site config → Schedule tab → toggle a window → Save. Then `GET /api/v1/sites/{id}` and confirm `monitoring_schedule` reflects the change (works only after PR #51). |
| **Notes** | Backend handler is correct (writes ONLY the `monitoring_schedule` jsonb column — routing through `PUT /api/v1/sites/{id}` used to drop the field and blank name/address). But on this branch the frontend hook `useUpdateSiteMonitoringSchedule` (useSites.ts:76) calls `PUT /api/sites/{id}/monitoring-schedule` — missing the `/v1` prefix — so every save 404s (api-coverage Table C). The one-line prefix fix lands in PR #51; until it merges, Status stays partial. Admin/supervisor role required server-side. |

## Site contacts {#site-contacts}

| Field | Value |
|---|---|
| **ID** | `site-contacts` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Customer-maintained call list per site (name, role, phone, email, notify-on-alarm) — who the SOC calls when an alarm fires. Self-service editor at /portal/sites/{id}/contacts. |
| **Frontend** | `frontend/src/app/portal/sites/[id]/contacts/page.tsx` |
| **Routes** | `GET /api/v1/sites/{id}/contacts` · `PUT /api/v1/sites/{id}/contacts` |
| **Tables** | sites |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Open /portal/sites/&lt;id&gt;/contacts as a site_manager → add a row → Save → reload; row persists. |
| **Notes** | Stored as a `customer_contacts` jsonb array on the sites row (migration 0012). Read allowed for any role with site access; writes gated to admin / soc_supervisor / site_manager. Out-of-scope site IDs return 404, not 403 (no existence leak). Distinct from SOC-side SOPs (area 07). |

## Device & user assignment to sites {#site-assignments}

| Field | Value |
|---|---|
| **ID** | `site-assignments` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Attach cameras (and speakers) from the master device registry to a site with a location label; assign company users to sites for portal access scoping. |
| **Frontend** | `frontend/src/components/admin/AssignCameraModal.tsx` · `frontend/src/hooks/useCameraAssignment.ts` · `frontend/src/components/admin/CustomerAccessModal.tsx` |
| **Routes** | `POST /api/v1/sites/{siteId}/camera-assignments` · `DELETE /api/v1/sites/{siteId}/camera-assignments/{cameraId}` · `POST /api/v1/sites/{siteId}/speaker-assignments` · `DELETE /api/v1/sites/{siteId}/speaker-assignments/{speakerId}` · `GET /api/v1/cameras` · `GET /api/v1/speakers` |
| **Tables** | cameras, speakers, device_assignments |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Admin → site config → Cameras tab → check a camera onto the site. Site detail page camera list shows it; uncheck and it disappears. |
| **Notes** | Camera/speaker assign/unassign work end-to-end (UPDATE `cameras.site_id` + a `device_assignments` history row). Three broken limbs, all api-coverage Table C 404s: (1) `useCameraAssignments` GETs `/api/v1/sites/{*}/camera-assignments` — no route, but no component calls it; (2) `useCreateCamera`/`useDeleteCamera` hit `POST`/`DELETE /api/v1/cameras` — no routes, hooks unused (camera CRUD really lives at `/api/cameras/`, area 03); (3) user-to-site assignment is broken end-to-end: `CustomerAccessModal` calls `GET`/`POST`/`DELETE /api/v1/sites/{*}/users` which don't exist, so the modal hangs at "Loading users…" forever. The working path for user scoping is `users.assigned_site_ids` via `PATCH /api/users/{id}` (area 05) — fix the modal to use that or add the routes. Speaker tab surfaces ride the parked speakers feature (area 03, flag `speakers`). |

## Portal dashboard {#portal-dashboard}

| Field | Value |
|---|---|
| **ID** | `portal-dashboard` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | The customer landing page at /portal: KPI cards (cameras online, open incidents), site card/list grid, recent-incident feeds, and CSV exports, polling live data every 30s. |
| **Frontend** | `frontend/src/app/portal/page.tsx` · `frontend/src/app/portal/layout.tsx` |
| **Routes** | `GET /api/v1/sites` · `GET /api/v1/incidents` · `GET /api/v1/portal/summary` |
| **Tables** | sites, incidents |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Log in as a customer user → /portal. Camera-online KPI matches admin counts; click a site card → drill-down loads; Export CSV downloads real rows. |
| **Notes** | Sites/incidents data and CSV exports are real API. Not working because of baked-in fakes: org selector hardcoded "Turner Construction"; the 7-day PPE trend chart is `Math.random()` mock; the "↓ 1 from yesterday" delta is hardcoded; most sidebar nav items (Sites, Incidents, Trends, Camera Activity, SOC Events, Generate Report, Downloads) and all Quick Reports buttons are dead. `PendingReviewQueue` / `HandledForYouPanel` are parked compliance surfaces (area 08, flag `compliance`) still rendered inline here. MVP cleanup = strip/gate the fakes, not add backend. |

## Incidents list & detail {#incidents}

| Field | Value |
|---|---|
| **ID** | `incidents` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Customer-visible incident records (grouped/dispositioned alarms): filterable list feeding the portal dashboard and a per-incident detail page with severity, site, camera, and timeline metadata. |
| **Frontend** | `frontend/src/hooks/useIncidents.ts` · `frontend/src/app/portal/incidents/[id]/page.tsx` |
| **Routes** | `GET /api/v1/incidents` · `GET /api/v1/incidents/{id}` |
| **Tables** | incidents, alarms |
| **Flag** | — |
| **Docs** | [id-conventions.md](../id-conventions.md) |
| **Smoke test** | /portal → Recent Incidents → click one. Detail page renders the real incident ID, severity, site, and timestamps from the API. |
| **Notes** | List + detail are real and RBAC-scoped (customers only see own-org sites; 404 not 403 on probes). Cannot be `working`: status updates and comments are dead — `PUT /api/v1/incidents/{*}/status` and `POST /api/v1/incidents/{*}/comments` have no backend routes (Table C 404s), and the `useUpdateIncidentStatus`/`useAddComment` hooks have no component callers anyway. Detail-page buttons (Escalate, Mark Resolved, Export PDF, Escalate to HSE) have no onClick handlers, and the "Video Evidence" viewport is a staged CSS scene, not real footage. SOC-side acknowledge/dispositions live in area 07. |

## Portal history {#portal-history}

| Field | Value |
|---|---|
| **ID** | `portal-history` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Customer NVR history view at /portal/history: unified, RBAC-scoped event search across the caller's cameras (last 24h default), in-place clip playback, one-click evidence export per row. |
| **Frontend** | `frontend/src/app/portal/history/page.tsx` |
| **Routes** | `GET /api/search/events` · `GET /api/cameras/` · `GET /api/events/{id}/export` · `GET /api/search/semantic` |
| **Tables** | events, cameras |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | /portal/history → events for last 24h render with camera filter; click a row → clip plays via resolved playback_url; Export downloads the evidence ZIP. |
| **Notes** | Evidence export builds the `GET /api/events/{id}/export` URL directly (api-coverage Table B — static scan can't see href-style callers; the route exists). The semantic tab calls `GET /api/search/semantic`, which only returns results when the VLM indexer has run — semantic search is parked (area 08, flag `semantic_search`); consider gating just that tab so the core event history ships clean. |

## Support tickets {#support-tickets}

| Field | Value |
|---|---|
| **ID** | `support-tickets` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Customer-to-SOC ticket threads: a floating chat-style widget on every portal page, mirrored by a supervisor inbox tab on /reports. 30s polling on both sides. |
| **Frontend** | `frontend/src/components/portal/SupportWidget.tsx` · `frontend/src/components/reports/SupportTicketsCard.tsx` |
| **Routes** | `GET /api/support/tickets` · `POST /api/support/tickets` · `GET /api/support/tickets/{id}` · `PATCH /api/support/tickets/{id}` · `POST /api/support/tickets/{id}/messages` |
| **Tables** | support_tickets, support_messages |
| **Flag** | `support_tickets` |
| **Docs** | — |
| **Smoke test** | On test.ironsight with flags on: portal bubble → new ticket → reply from /reports Support tab → reply appears in the widget within one 30s poll. |
| **Notes** | Backend and customer widget fully wired (all five routes matched in api-coverage Table A); ticket creation notifies via the dispatcher. Gotcha: the widget is mounted unconditionally in `app/portal/layout.tsx` — NOT yet behind the `support_tickets` flag, so it ships customer-visible at MVP unless gated. The supervisor inbox is only reachable through /reports, which is whole-page-gated behind `operator_console`, so flipping `support_tickets` on alone gives customers a widget no one answers in-app. The /reports card path is wired but not smoke-verified on test. Revival cost: near zero — gate the widget, verify one round-trip. |

## Evidence share links {#evidence-shares}

| Field | Value |
|---|---|
| **ID** | `evidence-shares` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | Supervisors mint expiring public links to incident evidence for police/insurers; every open is logged for chain of custody; links can be revoked. |
| **Frontend** | `frontend/src/components/reports/EvidenceSharesCard.tsx` · `frontend/src/app/evidence/[token]/page.tsx` |
| **Routes** | `POST /api/v1/incidents/{id}/share` · `GET /api/v1/incidents/{id}/shares` · `DELETE /api/v1/shares/{token}` · `GET /share/{token}` |
| **Tables** | evidence_shares, evidence_share_opens, evidence_manifests |
| **Flag** | `evidence_sharing` |
| **Docs** | [chain-of-custody.md](../chain-of-custody.md) |
| **Smoke test** | /reports (operator_console on) → Evidence shares tab → enter an incident ID → create link → `curl /share/<token>` returns share JSON; revoke → same curl 404s. |
| **Notes** | Create/list/revoke are real (supervisor/admin only; TTL clamped to 90d, default 7d; a chain-of-custody manifest is written per share). `GET /share/{token}` serves share *metadata JSON* and logs opens — it is not a viewer. The customer-facing viewer `app/evidence/[token]/page.tsx` renders `mockFetchEvidence` hardcoded data (the "Southgate Power Station" scenario) and never calls the backend, and the page is not yet gated behind `evidence_sharing`. Revival cost: point the page at `GET /share/{token}`, render the clip via media mint, add the gate — backend needs nothing. Cross-ref [[incidents]]; manifests detail in area 09. |

## Public status page {#status-page}

| Field | Value |
|---|---|
| **ID** | `status-page` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Unauthenticated /status page showing platform health (operational/degraded/critical), camera online counts, and last-hour alarm volume — aggregates only, no per-customer data. |
| **Frontend** | `frontend/src/app/status/page.tsx` |
| **Routes** | `GET /api/status` |
| **Tables** | cameras, alarms |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Open /status in a logged-out browser → headline state + camera counts render; leave open 60s and confirm it refreshes. |
| **Notes** | Headline state is derived server-side so threshold changes need no frontend deploy. Response carries a 60s Cache-Control for refresh storms. Frontend polls every 30s. Deliberately leaks nothing per-tenant — keep it that way when adding metrics. |

## Feature flags service {#feature-flags}

| Field | Value |
|---|---|
| **ID** | `feature-flags` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Deploy-wide flag map that hides parked features at MVP: backend returns DefaultFeatureFlags merged with the FEATURES_OVERRIDE env var; the frontend hydrates once per page load and gates nav/pages via FeatureGate / FeaturePageGate. |
| **Frontend** | `frontend/src/lib/feature-flags.ts` · `frontend/src/components/shared/FeatureGate.tsx` |
| **Routes** | `GET /api/v1/features` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [configuration.md](../configuration.md) |
| **Smoke test** | `curl /api/v1/features` → 14 keys, parked ones false. Set `FEATURES_OVERRIDE=analytics=true`, restart, reload → /analytics renders instead of 404ing. |
| **Notes** | Rewritten in the Phase D descope pass (this session): the old hardcoded 4-flag stub and the localStorage-based frontend shim are gone. Defaults fail closed — until the fetch resolves, all parked flags read false, so a slow response can never flash a parked page open. Rollout is the open work: only /analytics, /search, /reports use the gates so far; remaining parked pages (/operator, /evidence/[token], portal compliance tab, admin parked tabs) and the SupportWidget still need gating — tracked per feature. Per-company flags (`organizations.features` jsonb) exist in schema, deliberately unwired. Dead code: a legacy `getFeatureFlags(siteId)` duplicate in `lib/ironsight-api.ts:879` (no callers) should be deleted. |
