# API coverage matrix

<!-- GENERATED FILE — DO NOT HAND-EDIT. Regenerate: go run ./cmd/docgen -write -->

Extracted from `internal/api/router.go` and `frontend/src/**`. 177 backend routes, 171 resolvable frontend call sites.

- **Matched** (route has ≥1 frontend caller): 125
- **Backend-only** (no frontend caller found): 52
- **Frontend-only** (no backend route — likely 404): 33

## A. Matched routes

| Route | Handler | Callers | Features |
|---|---|---|---|
| `GET /api/audio-messages` | `HandleListAudioMessages` | `listAudioMessages` (frontend/src/lib/api.ts:1019) | `speakers-audio` |
| `POST /api/audio-messages` | `HandleUploadAudioMessage` | `uploadAudioMessage` (frontend/src/lib/api.ts:1029) | `speakers-audio` |
| `DELETE /api/audio-messages/{id}` | `HandleDeleteAudioMessage` | `deleteAudioMessage` (frontend/src/lib/api.ts:1038) | `speakers-audio` |
| `GET /api/audit` | `HandleQueryAuditLog` | `queryAuditLog` (frontend/src/lib/api.ts:1082) | `audit-log` |
| `GET /api/auth/csrf` | `HandleCSRF` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:155) | `session-csrf` |
| `GET /api/auth/ws-ticket` | `HandleWSTicket` | `(module)` (frontend/src/app/page.tsx:277)<br>`(module)` (frontend/src/lib/ws-alerts.ts:116) | `alert-websocket-stream` `session-csrf` |
| `GET /api/cameras/` | `HandleListCameras` | `useMasterCameras` (frontend/src/hooks/useCameraAssignment.ts:30)<br>`listCameras` (frontend/src/lib/api.ts:457) | `live-camera-grid` `camera-crud` `portal-history` |
| `POST /api/cameras/` | `HandleCreateCamera` | `createCamera` (frontend/src/lib/api.ts:464) | `camera-crud` |
| `DELETE /api/cameras/{id}` | `HandleDeleteCamera` | `deleteCamera` (frontend/src/lib/api.ts:491) | `camera-crud` |
| `GET /api/cameras/{id}` | `HandleGetCamera` | `(module)` (frontend/src/app/popout/[cameraId]/page.tsx:16) | `live-popout` `camera-crud` |
| `PATCH /api/cameras/{id}` | `HandleUpdateCamera` | `updateCamera` (frontend/src/lib/api.ts:482) | `camera-crud` |
| `POST /api/cameras/{id}/deterrence` | `HandleDeterrence` | `fireDeterrence` (frontend/src/lib/api.ts:147) | `deterrence-outputs` |
| `GET /api/cameras/{id}/milesight/config/{panel}` | `HandleMilesightGet` | `milesightGet` (frontend/src/lib/milesight.ts:17) | `milesight-config` |
| `PUT /api/cameras/{id}/milesight/config/{panel}` | `HandleMilesightSet` | `milesightSet` (frontend/src/lib/milesight.ts:23) | `milesight-config` |
| `POST /api/cameras/{id}/milesight/ptz/preset/goto` | `HandlePTZPresetGoto` | `milesightPTZGoto` (frontend/src/lib/milesight.ts:43) | `ptz-controls` `milesight-config` |
| `POST /api/cameras/{id}/milesight/reboot` | `HandleMilesightReboot` | `milesightReboot` (frontend/src/lib/milesight.ts:38) | `milesight-config` |
| `POST /api/cameras/{id}/ptz/move` | `HandlePTZMove` | `ptzMove` (frontend/src/lib/api.ts:652) | `ptz-controls` |
| `POST /api/cameras/{id}/ptz/prewarm` | `HandlePTZPrewarm` | `ptzPrewarm` (frontend/src/lib/api.ts:668) | `ptz-controls` |
| `POST /api/cameras/{id}/ptz/stop` | `HandlePTZStop` | `ptzStop` (frontend/src/lib/api.ts:661) | `ptz-controls` |
| `POST /api/cameras/{id}/reboot` | `HandleRebootCamera` | `rebootCamera` (frontend/src/lib/api.ts:497) | `camera-crud` |
| `GET /api/cameras/{id}/sd/status` | `HandleSDStatus` | `getSDStatus` (frontend/src/lib/api.ts:448) | `sd-card-status` |
| `GET /api/cameras/{id}/vca/rules` | `HandleListVCARules` | `(module)` (frontend/src/components/operator/ActiveAlarmView.tsx:1571)<br>`(module)` (frontend/src/components/operator/AlarmVideoFeed.tsx:48)<br>`listVCARules` (frontend/src/lib/api.ts:530) | `vca-rules` |
| `POST /api/cameras/{id}/vca/rules` | `HandleCreateVCARule` | `createVCARule` (frontend/src/lib/api.ts:536) | `vca-rules` |
| `DELETE /api/cameras/{id}/vca/rules/{ruleId}` | `HandleDeleteVCARule` | `deleteVCARule` (frontend/src/lib/api.ts:555) | `vca-rules` |
| `PUT /api/cameras/{id}/vca/rules/{ruleId}` | `HandleUpdateVCARule` | `updateVCARule` (frontend/src/lib/api.ts:546) | `vca-rules` |
| `GET /api/cameras/{id}/vca/snapshot` | `HandleVCASnapshot` | `CameraSection` (frontend/src/components/admin/AssignCameraModal.tsx:34)<br>`(module)` (frontend/src/components/operator/ActiveAlarmView.tsx:1628)<br>`(module)` (frontend/src/components/operator/AlarmVideoFeed.tsx:105) | `vca-rules` |
| `POST /api/discover` | `HandleDiscover` | `discoverCameras` (frontend/src/lib/api.ts:564) | `onvif-discovery` |
| `POST /api/discover/preview` | `HandleDiscoverPreview` | `getDevicePreview` (frontend/src/lib/api.ts:571) | `onvif-discovery` |
| `GET /api/events` | `HandleQueryEvents` | `(module)` (frontend/src/components/AnalyticsDashboard.tsx:44)<br>`queryEvents` (frontend/src/lib/api.ts:606) | `event-ingestion` |
| `GET /api/exports` | `HandleListExports` | `listExports` (frontend/src/lib/api.ts:643) | `clip-export` |
| `POST /api/exports` | `HandleCreateExport` | `createExport` (frontend/src/lib/api.ts:634) | `clip-export` |
| `GET /api/me/notifications` | `HandleListMyNotificationSubs` | `(module)` (frontend/src/app/portal/notifications/page.tsx:86) | `notification-preferences` |
| `PUT /api/me/notifications` | `HandleUpsertMyNotificationSub` | `(module)` (frontend/src/app/portal/notifications/page.tsx:100) | `notification-preferences` |
| `POST /api/media/mint` | `HandleMediaMint` | `mintMediaToken` (frontend/src/lib/media.ts:49) | `media-token-auth` |
| `GET /api/playback/{id}` | `HandlePlayback` | `fetchPlaybackSegments` (frontend/src/lib/api.ts:1095) | `playback-timeline` |
| `GET /api/recording/health` | `HandleRecordingHealth` | `getRecordingHealth` (frontend/src/lib/api.ts:297) | `recording-health` |
| `GET /api/reports/sla` | `HandleSLAReport` | `getSLAReport` (frontend/src/lib/ironsight-api.ts:603) | `operator-presence-metrics` |
| `GET /api/search/events` | `HandleSearchEvents` | `searchEvents` (frontend/src/lib/api.ts:206) | `portal-history` |
| `POST /api/search/frames` | `HandleSearchFrames` | `searchFrames` (frontend/src/lib/ironsight-api.ts:135) | `semantic-search` |
| `GET /api/search/semantic` | `HandleSemanticSearch` | `searchSemantic` (frontend/src/lib/api.ts:271) | `portal-history` `semantic-search` |
| `GET /api/settings` | `HandleGetSettings` | `getSettings` (frontend/src/lib/api.ts:757) | `system-settings` |
| `PUT /api/settings` | `HandleUpdateSettings` | `updateSettings` (frontend/src/lib/api.ts:763) | `system-settings` |
| `GET /api/speaker-info` | `HandleBulkInfo` | `getSpeakerInfo` (frontend/src/lib/api.ts:1043) | `speakers-audio` |
| `GET /api/speakers` | `HandleListSpeakers` | `listSpeakers` (frontend/src/lib/api.ts:984) | `speakers-audio` |
| `POST /api/speakers` | `HandleCreateSpeaker` | `createSpeaker` (frontend/src/lib/api.ts:992) | `speakers-audio` |
| `POST /api/speakers/stop` | `HandleStopPlayback` | `stopSpeakerPlayback` (frontend/src/lib/api.ts:1015) | `speakers-audio` |
| `DELETE /api/speakers/{id}` | `HandleDeleteSpeaker` | `deleteSpeaker` (frontend/src/lib/api.ts:1002) | `speakers-audio` |
| `POST /api/speakers/{id}/play/{messageId}` | `HandlePlayMessage` | `playSpeakerMessage` (frontend/src/lib/api.ts:1007) | `speakers-audio` |
| `GET /api/status` | `HandlePublicStatus` | `(module)` (frontend/src/app/status/page.tsx:53) | `status-page` |
| `GET /api/storage/browse` | `HandleBrowsePath` | `browsePath` (frontend/src/lib/api.ts:914) | `storage-locations` |
| `GET /api/storage/disk-usage` | `HandleGetDiskUsage` | `getDiskUsage` (frontend/src/lib/api.ts:920) | `storage-locations` |
| `GET /api/storage/drives` | `HandleListDrives` | `listDrives` (frontend/src/lib/api.ts:906) | `storage-locations` |
| `GET /api/storage/locations` | `HandleListStorageLocations` | `listStorageLocations` (frontend/src/lib/api.ts:926) | `storage-locations` |
| `POST /api/storage/locations` | `HandleCreateStorageLocation` | `createStorageLocation` (frontend/src/lib/api.ts:932) | `storage-locations` |
| `DELETE /api/storage/locations/{id}` | `HandleDeleteStorageLocation` | `deleteStorageLocation` (frontend/src/lib/api.ts:951) | `storage-locations` |
| `PUT /api/storage/locations/{id}` | `HandleUpdateStorageLocation` | `updateStorageLocation` (frontend/src/lib/api.ts:942) | `storage-locations` |
| `GET /api/storage/status` | `(inline)` | `(module)` (frontend/src/app/page.tsx:99) | `storage-locations` |
| `GET /api/support/tickets` | `HandleListSupportTickets` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:85)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:67) | `support-tickets` |
| `POST /api/support/tickets` | `HandleCreateSupportTicket` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:134) | `support-tickets` |
| `GET /api/support/tickets/{id}` | `HandleGetSupportTicket` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:108)<br>`(module)` (frontend/src/components/portal/SupportWidget.tsx:157)<br>`(module)` (frontend/src/components/portal/SupportWidget.tsx:180)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:88)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:100)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:117) | `support-tickets` |
| `PATCH /api/support/tickets/{id}` | `HandleUpdateSupportTicket` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:153)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:131) | `support-tickets` |
| `POST /api/support/tickets/{id}/messages` | `HandleSupportTicketReply` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:172)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:112) | `support-tickets` |
| `GET /api/system/health` | `HandleSystemHealth` | `getSystemHealth` (frontend/src/lib/api.ts:751) | `system-health` |
| `GET /api/system/services` | `HandleServicesHealth` | `getServicesHealth` (frontend/src/lib/api.ts:344) | `ai-services-health` |
| `GET /api/system/services/timeseries` | `HandleAIMetricsTimeseries` | `getAIMetricsTimeseries` (frontend/src/lib/api.ts:416) | `ai-services-health` `ai-runtime-metrics` |
| `GET /api/timeline` | `HandleGetTimeline` | `getTimeline` (frontend/src/lib/api.ts:626) | `playback-timeline` |
| `GET /api/timeline/coverage` | `HandleGetCoverage` | `fetchCoverage` (frontend/src/lib/api.ts:700) | `playback-timeline` |
| `GET /api/users` | `HandleListUsers` | `listUsers` (frontend/src/lib/api.ts:809) | `users-roles` `site-assignments` |
| `POST /api/users` | `HandleCreateUser` | `createUser` (frontend/src/lib/api.ts:815) | `users-roles` |
| `DELETE /api/users/{id}` | `HandleDeleteUser` | `deleteUser` (frontend/src/lib/api.ts:825) | `users-roles` |
| `PATCH /api/users/{id}` | `HandleUpdateUserProfile` | `updateUserProfile` (frontend/src/lib/api.ts:848) | `users-roles` `site-assignments` |
| `PATCH /api/users/{id}/password` | `HandleUpdateUserPassword` | `updateUserPassword` (frontend/src/lib/api.ts:830) | `users-roles` |
| `PATCH /api/users/{id}/role` | `HandleUpdateUserRole` | `updateUserRole` (frontend/src/lib/api.ts:839) | `users-roles` |
| `POST /api/v1/alarms/{alarmId}/ai-feedback` | `(inline)` | `submitAIFeedback` (frontend/src/lib/ironsight-api.ts:561) | `alarm-ai-feedback` |
| `POST /api/v1/alarms/{alarmId}/escalate` | `HandleEscalateAlarm` | `escalateAlarm` (frontend/src/lib/ironsight-api.ts:554) | `alarm-escalation` |
| `GET /api/v1/alerts` | `(inline)` | `getAlerts` (frontend/src/lib/ironsight-api.ts:121)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `alert-feed-acknowledge` `operator-console-shell` |
| `GET /api/v1/cameras` | `HandleListAllPlatformCameras` | `useMasterCameras` (frontend/src/hooks/useCameraAssignment.ts:38)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `site-assignments` |
| `GET /api/v1/companies` | `HandleListOrganizations` | `getCompanies` (frontend/src/lib/ironsight-api.ts:163)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `companies-management` |
| `POST /api/v1/companies` | `HandleCreateOrganization` | `createCompany` (frontend/src/lib/ironsight-api.ts:171) | `companies-management` |
| `GET /api/v1/companies/{companyId}/users` | `HandleListCompanyUsers` | `getCompanyUsers` (frontend/src/lib/ironsight-api.ts:180) | `companies-management` |
| `GET /api/v1/detections` | `HandleListDetections` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `yolo-detection` |
| `GET /api/v1/device-history` | `HandleGetDeviceHistory` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | — |
| `GET /api/v1/events` | `HandleListSecurityEvents` | `listSecurityEvents` (frontend/src/lib/ironsight-api.ts:550)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `alarm-investigation` |
| `POST /api/v1/events` | `HandleCreateSecurityEvent` | `createSecurityEvent` (frontend/src/lib/ironsight-api.ts:524) | `notify-dispatch` `alarm-investigation` |
| `POST /api/v1/events/{id}/verify` | `HandleVerifySecurityEvent` | `verifySecurityEvent` (frontend/src/lib/ironsight-api.ts:676) | — |
| `GET /api/v1/features` | `HandleFeatureFlags` | `(module)` (frontend/src/lib/feature-flags.ts:68)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `feature-flags` |
| `GET /api/v1/handoffs` | `HandleListHandoffs` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770)<br>`getPendingHandoffs` (frontend/src/lib/ironsight-api.ts:337) | `shift-handoffs` |
| `POST /api/v1/handoffs` | `HandleCreateHandoff` | `createHandoff` (frontend/src/lib/ironsight-api.ts:341) | `shift-handoffs` |
| `GET /api/v1/incidents` | `HandleListIncidents` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770)<br>`getIncidents` (frontend/src/lib/ironsight-api.ts:92) | `incidents` `portal-dashboard` |
| `GET /api/v1/incidents/active` | `(inline)` | `getActiveIncidents` (frontend/src/lib/ironsight-api.ts:125)<br>`getIncidentDetail` (frontend/src/lib/ironsight-api.ts:129)<br>`getIncident` (frontend/src/lib/ironsight-api.ts:96) | `alert-feed-acknowledge` `incidents` |
| `GET /api/v1/incidents/{id}` | `HandleGetIncident` | `getActiveIncidents` (frontend/src/lib/ironsight-api.ts:125)<br>`getIncidentDetail` (frontend/src/lib/ironsight-api.ts:129)<br>`getIncident` (frontend/src/lib/ironsight-api.ts:96) | `alert-feed-acknowledge` `incidents` |
| `POST /api/v1/incidents/{id}/share` | `HandleCreateEvidenceShare` | `createEvidenceShareLink` (frontend/src/lib/ironsight-api.ts:732) | `evidence-shares` |
| `GET /api/v1/incidents/{id}/shares` | `HandleListEvidenceShares` | `listIncidentShares` (frontend/src/lib/ironsight-api.ts:694) | `evidence-shares` |
| `POST /api/v1/incidents/{incidentId}/acknowledge` | `(inline)` | `(module)` (frontend/src/components/operator/ActiveAlarmView.tsx:254) | `alert-feed-acknowledge` `alarm-investigation` |
| `GET /api/v1/model-versions` | `HandleListModelVersions` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `yolo-detection` |
| `GET /api/v1/operators` | `HandleListOperators` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770) | `operator-console-shell` |
| `GET /api/v1/operators/current` | `HandleGetCurrentOperator` | `getCurrentOperator` (frontend/src/lib/ironsight-api.ts:259) | `operator-console-shell` |
| `GET /api/v1/portal/compliance/report.pdf` | `HandleComplianceReportPDF` | `downloadComplianceReport` (frontend/src/lib/api.ts:1236) | `compliance-dashboard` |
| `GET /api/v1/portal/compliance/summary` | `HandleComplianceSummary` | `getComplianceSummary` (frontend/src/lib/api.ts:1220) | `compliance-dashboard` |
| `GET /api/v1/portal/pending-review` | `HandleListPendingReview` | `getPendingReview` (frontend/src/lib/api.ts:1118) | `ppe-pending-review` |
| `POST /api/v1/portal/pending-review/{id}/review` | `HandleReviewPendingEntry` | `submitReview` (frontend/src/lib/api.ts:1127) | `ppe-pending-review` |
| `GET /api/v1/portal/summary` | `HandlePortalSummary` | `getPortalSummary` (frontend/src/lib/ironsight-api.ts:109) | `portal-dashboard` |
| `DELETE /api/v1/shares/{token}` | `HandleRevokeEvidenceShare` | `revokeEvidenceShareLink` (frontend/src/lib/ironsight-api.ts:751) | `evidence-shares` |
| `GET /api/v1/sites` | `HandleListSites` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770)<br>`getSites` (frontend/src/lib/ironsight-api.ts:67) | `portal-dashboard` `sites-crud` `operator-console-shell` |
| `POST /api/v1/sites` | `HandleCreateSiteP` | `createSite` (frontend/src/lib/ironsight-api.ts:186) | `sites-crud` |
| `GET /api/v1/sites/locks` | `HandleSiteLocks` | `getSiteLocks` (frontend/src/lib/ironsight-api.ts:269)<br>`getSite` (frontend/src/lib/ironsight-api.ts:71) | `sites-crud` `alarm-investigation` `site-locks` |
| `DELETE /api/v1/sites/{id}` | `HandleDeleteSiteP` | `deleteSite` (frontend/src/lib/ironsight-api.ts:200) | `sites-crud` |
| `GET /api/v1/sites/{id}` | `HandleGetSite` | `getSiteLocks` (frontend/src/lib/ironsight-api.ts:269)<br>`getSite` (frontend/src/lib/ironsight-api.ts:71) | `sites-crud` `alarm-investigation` `site-locks` |
| `PUT /api/v1/sites/{id}` | `HandleUpdateSite` | `updateSite` (frontend/src/lib/ironsight-api.ts:193) | `sites-crud` |
| `GET /api/v1/sites/{id}/contacts` | `HandleListSiteContacts` | `(module)` (frontend/src/app/portal/sites/[id]/contacts/page.tsx:42) | `site-contacts` |
| `PUT /api/v1/sites/{id}/contacts` | `HandleUpdateSiteContacts` | `(module)` (frontend/src/app/portal/sites/[id]/contacts/page.tsx:51) | `site-contacts` |
| `PUT /api/v1/sites/{id}/monitoring-schedule` | `HandleUpdateSiteMonitoringSchedule` | `useUpdateSiteMonitoringSchedule` (frontend/src/hooks/useSites.ts:75) | `monitoring-schedule` |
| `POST /api/v1/sites/{siteId}/camera-assignments` | `HandleAssignCamera` | `assignCameraToSite` (frontend/src/lib/ironsight-api.ts:210) | `site-assignments` |
| `DELETE /api/v1/sites/{siteId}/camera-assignments/{cameraId}` | `HandleUnassignCamera` | `unassignCamera` (frontend/src/lib/ironsight-api.ts:217) | `site-assignments` |
| `GET /api/v1/sites/{siteId}/cameras` | `HandleGetSiteCameras` | `getSiteCameras` (frontend/src/lib/ironsight-api.ts:75) | `sites-crud` `alarm-investigation` |
| `GET /api/v1/sites/{siteId}/sops` | `HandleListSiteSOPs` | `getSiteSOPs` (frontend/src/lib/ironsight-api.ts:296) | `alarm-investigation` `site-sops` |
| `POST /api/v1/sites/{siteId}/sops` | `HandleCreateSiteSOP` | `createSiteSOP` (frontend/src/lib/ironsight-api.ts:300) | `site-sops` |
| `POST /api/v1/sites/{siteId}/speaker-assignments` | `HandleAssignSpeaker` | `assignSpeakerToSite` (frontend/src/lib/ironsight-api.ts:227) | `speakers-audio` `site-assignments` |
| `DELETE /api/v1/sites/{siteId}/speaker-assignments/{speakerId}` | `HandleUnassignSpeaker` | `unassignSpeaker` (frontend/src/lib/ironsight-api.ts:234) | `speakers-audio` `site-assignments` |
| `DELETE /api/v1/sops/{id}` | `HandleDeleteSiteSOP` | `deleteSiteSOP` (frontend/src/lib/ironsight-api.ts:314) | `site-sops` |
| `PUT /api/v1/sops/{id}` | `HandleUpdateSiteSOP` | `updateSiteSOP` (frontend/src/lib/ironsight-api.ts:307) | `site-sops` |
| `GET /api/v1/speakers` | `HandleListAllPlatformSpeakers` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:770)<br>`getAllSpeakers` (frontend/src/lib/ironsight-api.ts:223) | `speakers-audio` `site-assignments` |
| `POST /auth/login` | `HandleLogin` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:172) | `password-login` |
| `POST /auth/logout` | `HandleLogout` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:213) | `session-csrf` |
| `GET /auth/me` | `HandleGetMe` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:133) | `session-csrf` |

## B. Backend-only routes

Routes with no frontend caller. Annotated routes are called by external clients by design; unannotated rows are either unwired backend surface or callers the static scan cannot resolve.

| Route | Handler | External caller | Features |
|---|---|---|---|
| `GET /api/admin/labeling/export` | `HandleExportLabeledDataset` | — | `labeling-queue` |
| `GET /api/admin/labeling/jobs` | `HandleListLabelJobs` | — | `labeling-queue` |
| `POST /api/admin/labeling/jobs/next` | `HandleClaimNextLabelJob` | — | `labeling-queue` |
| `POST /api/admin/labeling/jobs/{id}/claim` | `HandleClaimLabelJob` | — | `labeling-queue` |
| `POST /api/admin/labeling/jobs/{id}/label` | `HandleSubmitLabel` | — | `labeling-queue` |
| `GET /api/admin/labeling/stats` | `HandleLabelingStats` | — | `labeling-queue` |
| `POST /api/admin/reanalyze/` | `HandleAdminReanalyze` | — | `reanalysis-admin` |
| `GET /api/admin/reanalyze/{run_id}` | `HandleGetReanalyzeRun` | — | `reanalysis-admin` |
| `GET /api/audio-messages/file/{fileName}` | `HandleServeAudioFile` | — | — |
| `GET /api/audio-messages/{id}` | `HandleGetAudioMessage` | — | — |
| `POST /api/auth/mfa/confirm` | `HandleMFAConfirm` | — | `mfa-totp` |
| `POST /api/auth/mfa/disable` | `HandleMFADisable` | — | `mfa-totp` |
| `POST /api/auth/mfa/enroll` | `HandleMFAEnroll` | — | `mfa-totp` |
| `GET /api/bookmarks` | `HandleListBookmarks` | — | `bookmarks` |
| `POST /api/bookmarks` | `HandleCreateBookmark` | — | `bookmarks` |
| `DELETE /api/bookmarks/{id}` | `HandleDeleteBookmark` | — | `bookmarks` |
| `GET /api/cameras/{id}/detect` | `HandleDetectLatest` | — | — |
| `GET /api/cameras/{id}/detect/stream` | `HandleDetectionStream` | — | — |
| `GET /api/cameras/{id}/recordings` | `HandleGetRecordings` | — | `playback-timeline` |
| `GET /api/cameras/{id}/vca/pull` | `HandleVCAPull` | — | `vca-rules` |
| `POST /api/cameras/{id}/vca/pull` | `HandleVCAPull` | — | `vca-rules` |
| `POST /api/cameras/{id}/vca/sync` | `HandleSyncVCARules` | — | `vca-rules` |
| `ANY /api/cameras/{id}/web-ui` | `HandleCameraWebUIProxy` | camera web-UI iframe (browser-driven) | — |
| `ANY /api/cameras/{id}/web-ui/*` | `HandleCameraWebUIProxy` | camera web-UI iframe (browser-driven) | — |
| `GET /api/events/{id}/export` | `HandleEvidenceExport` | — | `clip-export` `portal-history` |
| `GET /api/health` | `(inline)` | Docker HEALTHCHECK / uptime monitors | `health-endpoint` |
| `POST /api/integrations/milesight/sense/{token}` | `HandleSenseWebhook` | camera push webhook (token-auth) | `sense-webhook` `event-ingestion` |
| `GET /api/live/{cameraID}/*` | `HandleLiveProxy` | — | `live-camera-grid` `live-hls-pipeline` `live-popout` |
| `GET /api/playback/{id}/playlist.m3u8` | `HandlePlaybackHLS` | — | `playback-timeline` |
| `GET /api/speakers/status` | `HandlePlaybackStatus` | — | `speakers-audio` |
| `GET /api/system/services/usage` | `HandleAIUsageBySite` | — | `ai-services-health` `ai-runtime-metrics` |
| `POST /api/users/{id}/mfa/reset` | `HandleAdminMFAReset` | — | `mfa-totp` |
| `POST /api/v1/companies/{companyId}/users` | `HandleCreateCompanyUser` | — | `companies-management` |
| `DELETE /api/v1/companies/{companyId}/users/{userId}` | `HandleDeleteCompanyUser` | — | `companies-management` |
| `DELETE /api/v1/companies/{id}` | `HandleDeleteOrganization` | — | `companies-management` |
| `PUT /api/v1/companies/{id}` | `HandleUpdateOrganization` | — | `companies-management` |
| `GET /api/v1/dispatch/queue` | `HandleDispatchQueue` | — | `dispatch-queue` |
| `GET /api/v1/evidence/manifests` | `HandleListManifests` | — | `evidence-manifests` |
| `GET /api/v1/evidence/manifests/{id}` | `HandleGetManifest` | — | `evidence-manifests` |
| `GET /api/v1/evidence/manifests/{id}/verify` | `HandleVerifyManifest` | — | `evidence-manifests` |
| `POST /api/v1/operators` | `HandleCreateOperator` | — | — |
| `GET /api/v1/operators/{operatorId}/handoffs` | `HandleOperatorHandoffs` | — | `shift-handoffs` |
| `GET /api/v1/portal/pending-review/{id}/frame` | `HandleServePPEFrame` | — | `ppe-pending-review` |
| `GET /api/v1/portal/person-tracks` | `HandleGetPersonTracks` | — | `person-tracking` |
| `PATCH /api/v1/sites/{id}/recording` | `HandleUpdateSiteRecording` | — | `recording-schedules` |
| `ANY /exports/*` | `http.StripPrefix` | evidence ZIP download links | `clip-export` |
| `GET /media/v1/{token}` | `HandleMediaServe` | minted media URLs consumed via <img>/<video> src | `hevc-transcode` `media-token-auth` |
| `GET /metrics` | `metricsHandler.ServeHTTP` | Prometheus scraper (network-trust) | `prometheus-metrics` |
| `GET /metrics` | `metricsHandler.ServeHTTP` | Prometheus scraper (network-trust) | `prometheus-metrics` |
| `GET /share/{token}` | `HandlePublicEvidenceShare` | public evidence share links (token-auth) | `evidence-shares` |
| `GET /ws` | `hub.HandleWebSocket` | — | `alert-websocket-stream` |
| `GET /ws/alerts` | `hub.HandleWebSocket` | — | `alert-websocket-stream` |

## C. Frontend-only calls

These call sites match no registered route — each one 404s at runtime and needs a fix or removal.

| Call | Where |
|---|---|
| `GET /api/cameras/{*}/compliance-rules` | `listComplianceRules` (frontend/src/lib/api.ts:1171) |
| `POST /api/cameras/{*}/compliance-rules` | `createComplianceRule` (frontend/src/lib/api.ts:1177) |
| `PUT /api/cameras/{*}/compliance-rules/{*}` | `updateComplianceRule` (frontend/src/lib/api.ts:1187) |
| `DELETE /api/cameras/{*}/compliance-rules/{*}` | `deleteComplianceRule` (frontend/src/lib/api.ts:1196) |
| `POST /api/cameras/{*}/ppe/zones` | `createPPEZone` (frontend/src/lib/api.ts:1147) |
| `GET /api/cameras/{*}/ppe/zones` | `listPPEZones` (frontend/src/lib/api.ts:1141) |
| `DELETE /api/cameras/{*}/ppe/zones/{*}` | `deletePPEZone` (frontend/src/lib/api.ts:1166) |
| `PUT /api/cameras/{*}/ppe/zones/{*}` | `updatePPEZone` (frontend/src/lib/api.ts:1157) |
| `DELETE /api/v1/alerts/{*}/claim` | `releaseAlert` (frontend/src/lib/ironsight-api.ts:290) |
| `PUT /api/v1/alerts/{*}/claim` | `claimAlert` (frontend/src/lib/ironsight-api.ts:283) |
| `POST /api/v1/cameras` | `useCreateCamera` (frontend/src/hooks/useCameraAssignment.ts:104) |
| `DELETE /api/v1/cameras/{*}` | `useDeleteCamera` (frontend/src/hooks/useCameraAssignment.ts:122) |
| `GET /api/v1/companies/{*}` | `getCompany` (frontend/src/lib/ironsight-api.ts:167) |
| `PUT /api/v1/handoffs/{*}/accept` | `acceptHandoff` (frontend/src/lib/ironsight-api.ts:345) |
| `GET /api/v1/integrations` | `getIntegrations` (frontend/src/lib/ironsight-api.ts:454) |
| `DELETE /api/v1/integrations/{*}` | `deleteIntegration` (frontend/src/lib/ironsight-api.ts:462) |
| `PATCH /api/v1/integrations/{*}` | `toggleIntegration` (frontend/src/lib/ironsight-api.ts:458) |
| `DELETE /api/v1/notifications/{*}` | `deleteNotificationRule` (frontend/src/lib/ironsight-api.ts:401) |
| `GET /api/v1/operators/metrics` | `getOperatorMetrics` (frontend/src/lib/ironsight-api.ts:368) |
| `PUT /api/v1/operators/presence` | `updatePresence` (frontend/src/lib/ironsight-api.ts:362) |
| `GET /api/v1/operators/presence` | `getOperatorPresence` (frontend/src/lib/ironsight-api.ts:358) |
| `GET /api/v1/reports/scheduled` | `getScheduledReports` (frontend/src/lib/ironsight-api.ts:374) |
| `PATCH /api/v1/reports/scheduled/{*}` | `toggleScheduledReport` (frontend/src/lib/ironsight-api.ts:378) |
| `GET /api/v1/search/suggest` | `getSearchSuggestions` (frontend/src/lib/ironsight-api.ts:154) |
| `GET /api/v1/sites/{*}/camera-assignments` | `getCameraAssignments` (frontend/src/lib/ironsight-api.ts:206) |
| `GET /api/v1/sites/{*}/compliance` | `getSiteCompliance` (frontend/src/lib/ironsight-api.ts:79) |
| `DELETE /api/v1/sites/{*}/lock` | `unlockSite` (frontend/src/lib/ironsight-api.ts:277) |
| `POST /api/v1/sites/{*}/lock` | `lockSite` (frontend/src/lib/ironsight-api.ts:273) |
| `GET /api/v1/sites/{*}/map` | `getSiteMap` (frontend/src/lib/ironsight-api.ts:320) |
| `PUT /api/v1/sites/{*}/map` | `updateSiteMap` (frontend/src/lib/ironsight-api.ts:324) |
| `GET /api/v1/sites/{*}/notifications` | `getNotificationRules` (frontend/src/lib/ironsight-api.ts:393) |
| `POST /api/v1/sites/{*}/notifications` | `createNotificationRule` (frontend/src/lib/ironsight-api.ts:397) |
| `GET /api/v1/sites/{*}/zones` | `getExclusionZones` (frontend/src/lib/ironsight-api.ts:407) |
