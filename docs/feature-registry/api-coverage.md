# API coverage matrix

<!-- GENERATED FILE — DO NOT HAND-EDIT. Regenerate: go run ./cmd/docgen -write -->

Extracted from `internal/api/router.go` and `frontend/src/**`. 175 backend routes, 207 resolvable frontend call sites.

- **Matched** (route has ≥1 frontend caller): 134
- **Backend-only** (no frontend caller found): 41
- **Frontend-only** (no backend route — likely 404): 58

## A. Matched routes

| Route | Handler | Callers | Features |
|---|---|---|---|
| `GET /api/audio-messages` | `HandleListAudioMessages` | `listAudioMessages` (frontend/src/lib/api.ts:1069) | — |
| `POST /api/audio-messages` | `HandleUploadAudioMessage` | `uploadAudioMessage` (frontend/src/lib/api.ts:1079) | — |
| `DELETE /api/audio-messages/{id}` | `HandleDeleteAudioMessage` | `deleteAudioMessage` (frontend/src/lib/api.ts:1088) | — |
| `GET /api/audit` | `HandleQueryAuditLog` | `queryAuditLog` (frontend/src/lib/api.ts:1132) | — |
| `GET /api/auth/csrf` | `HandleCSRF` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:155) | — |
| `GET /api/auth/ws-ticket` | `HandleWSTicket` | `(module)` (frontend/src/app/page.tsx:278)<br>`(module)` (frontend/src/lib/ws-alerts.ts:116) | — |
| `GET /api/bookmarks` | `HandleListBookmarks` | `listBookmarks` (frontend/src/lib/api.ts:1176) | — |
| `POST /api/bookmarks` | `HandleCreateBookmark` | `createBookmark` (frontend/src/lib/api.ts:1158) | — |
| `DELETE /api/bookmarks/{id}` | `HandleDeleteBookmark` | `deleteBookmark` (frontend/src/lib/api.ts:1182) | — |
| `GET /api/cameras/` | `HandleListCameras` | `useMasterCameras` (frontend/src/hooks/useCameraAssignment.ts:30)<br>`listCameras` (frontend/src/lib/api.ts:457)<br>`getIRONSightCameras` (frontend/src/lib/ironsight-api.ts:287) | — |
| `POST /api/cameras/` | `HandleCreateCamera` | `createCamera` (frontend/src/lib/api.ts:469) | — |
| `DELETE /api/cameras/{id}` | `HandleDeleteCamera` | `deleteCamera` (frontend/src/lib/api.ts:496) | — |
| `GET /api/cameras/{id}` | `HandleGetCamera` | `(module)` (frontend/src/app/popout/[cameraId]/page.tsx:16)<br>`getCamera` (frontend/src/lib/api.ts:464) | — |
| `PATCH /api/cameras/{id}` | `HandleUpdateCamera` | `updateCamera` (frontend/src/lib/api.ts:487) | — |
| `GET /api/cameras/{id}/detect` | `HandleDetectLatest` | `fetchDetections` (frontend/src/lib/api.ts:713) | — |
| `POST /api/cameras/{id}/deterrence` | `HandleDeterrence` | `fireDeterrence` (frontend/src/lib/api.ts:147) | — |
| `GET /api/cameras/{id}/milesight/config/{panel}` | `HandleMilesightGet` | `milesightGet` (frontend/src/lib/milesight.ts:17) | — |
| `PUT /api/cameras/{id}/milesight/config/{panel}` | `HandleMilesightSet` | `milesightSet` (frontend/src/lib/milesight.ts:23) | — |
| `POST /api/cameras/{id}/milesight/ptz/preset/goto` | `HandlePTZPresetGoto` | `milesightPTZGoto` (frontend/src/lib/milesight.ts:43) | — |
| `POST /api/cameras/{id}/milesight/reboot` | `HandleMilesightReboot` | `milesightReboot` (frontend/src/lib/milesight.ts:38) | — |
| `POST /api/cameras/{id}/ptz/move` | `HandlePTZMove` | `ptzMove` (frontend/src/lib/api.ts:669) | — |
| `POST /api/cameras/{id}/ptz/prewarm` | `HandlePTZPrewarm` | `ptzPrewarm` (frontend/src/lib/api.ts:685) | — |
| `POST /api/cameras/{id}/ptz/stop` | `HandlePTZStop` | `ptzStop` (frontend/src/lib/api.ts:678) | — |
| `POST /api/cameras/{id}/reboot` | `HandleRebootCamera` | `rebootCamera` (frontend/src/lib/api.ts:502) | — |
| `GET /api/cameras/{id}/sd/status` | `HandleSDStatus` | `getSDStatus` (frontend/src/lib/api.ts:448) | — |
| `GET /api/cameras/{id}/vca/pull` | `HandleVCAPull` | `vcaPullPreview` (frontend/src/lib/milesight.ts:299) | — |
| `POST /api/cameras/{id}/vca/pull` | `HandleVCAPull` | `vcaPullApply` (frontend/src/lib/milesight.ts:305) | — |
| `GET /api/cameras/{id}/vca/rules` | `HandleListVCARules` | `(module)` (frontend/src/components/operator/ActiveAlarmView.tsx:1571)<br>`(module)` (frontend/src/components/operator/AlarmVideoFeed.tsx:48)<br>`listVCARules` (frontend/src/lib/api.ts:535) | — |
| `POST /api/cameras/{id}/vca/rules` | `HandleCreateVCARule` | `createVCARule` (frontend/src/lib/api.ts:541) | — |
| `DELETE /api/cameras/{id}/vca/rules/{ruleId}` | `HandleDeleteVCARule` | `deleteVCARule` (frontend/src/lib/api.ts:560) | — |
| `PUT /api/cameras/{id}/vca/rules/{ruleId}` | `HandleUpdateVCARule` | `updateVCARule` (frontend/src/lib/api.ts:551) | — |
| `GET /api/cameras/{id}/vca/snapshot` | `HandleVCASnapshot` | `CameraSection` (frontend/src/components/admin/AssignCameraModal.tsx:34)<br>`(module)` (frontend/src/components/operator/ActiveAlarmView.tsx:1628)<br>`(module)` (frontend/src/components/operator/AlarmVideoFeed.tsx:105) | — |
| `POST /api/cameras/{id}/vca/sync` | `HandleSyncVCARules` | `syncVCARules` (frontend/src/lib/api.ts:564) | — |
| `POST /api/discover` | `HandleDiscover` | `discoverCameras` (frontend/src/lib/api.ts:575) | — |
| `POST /api/discover/preview` | `HandleDiscoverPreview` | `getDevicePreview` (frontend/src/lib/api.ts:582) | — |
| `GET /api/events` | `HandleQueryEvents` | `(module)` (frontend/src/components/AnalyticsDashboard.tsx:44)<br>`queryEvents` (frontend/src/lib/api.ts:617) | — |
| `GET /api/exports` | `HandleListExports` | `listExports` (frontend/src/lib/api.ts:654) | — |
| `POST /api/exports` | `HandleCreateExport` | `createExport` (frontend/src/lib/api.ts:645) | — |
| `GET /api/health` | `(inline)` | `healthCheck` (frontend/src/lib/api.ts:660) | — |
| `GET /api/me/notifications` | `HandleListMyNotificationSubs` | `(module)` (frontend/src/app/portal/notifications/page.tsx:86) | — |
| `PUT /api/me/notifications` | `HandleUpsertMyNotificationSub` | `(module)` (frontend/src/app/portal/notifications/page.tsx:100) | — |
| `POST /api/media/mint` | `HandleMediaMint` | `mintMediaToken` (frontend/src/lib/media.ts:52) | — |
| `GET /api/playback/{id}` | `HandlePlayback` | `fetchPlaybackSegments` (frontend/src/lib/api.ts:1194) | — |
| `GET /api/recording/health` | `HandleRecordingHealth` | `getRecordingHealth` (frontend/src/lib/api.ts:297) | — |
| `GET /api/search/events` | `HandleSearchEvents` | `searchEvents` (frontend/src/lib/api.ts:206) | — |
| `POST /api/search/frames` | `HandleSearchFrames` | `searchFrames` (frontend/src/lib/ironsight-api.ts:144) | — |
| `GET /api/search/semantic` | `HandleSemanticSearch` | `searchSemantic` (frontend/src/lib/api.ts:271) | — |
| `GET /api/settings` | `HandleGetSettings` | `getSettings` (frontend/src/lib/api.ts:801) | — |
| `PUT /api/settings` | `HandleUpdateSettings` | `updateSettings` (frontend/src/lib/api.ts:807) | — |
| `GET /api/speaker-info` | `HandleBulkInfo` | `getSpeakerInfo` (frontend/src/lib/api.ts:1093) | — |
| `GET /api/speakers` | `HandleListSpeakers` | `listSpeakers` (frontend/src/lib/api.ts:1028) | — |
| `POST /api/speakers` | `HandleCreateSpeaker` | `createSpeaker` (frontend/src/lib/api.ts:1036) | — |
| `GET /api/speakers/status` | `HandlePlaybackStatus` | `getSpeakerStatus` (frontend/src/lib/api.ts:1063) | — |
| `POST /api/speakers/stop` | `HandleStopPlayback` | `stopSpeakerPlayback` (frontend/src/lib/api.ts:1059) | — |
| `DELETE /api/speakers/{id}` | `HandleDeleteSpeaker` | `deleteSpeaker` (frontend/src/lib/api.ts:1046) | — |
| `POST /api/speakers/{id}/play/{messageId}` | `HandlePlayMessage` | `playSpeakerMessage` (frontend/src/lib/api.ts:1051) | — |
| `GET /api/status` | `HandlePublicStatus` | `(module)` (frontend/src/app/status/page.tsx:53) | — |
| `GET /api/storage/browse` | `HandleBrowsePath` | `browsePath` (frontend/src/lib/api.ts:958) | — |
| `GET /api/storage/disk-usage` | `HandleGetDiskUsage` | `getDiskUsage` (frontend/src/lib/api.ts:964) | — |
| `GET /api/storage/drives` | `HandleListDrives` | `listDrives` (frontend/src/lib/api.ts:950) | — |
| `GET /api/storage/locations` | `HandleListStorageLocations` | `listStorageLocations` (frontend/src/lib/api.ts:970) | — |
| `POST /api/storage/locations` | `HandleCreateStorageLocation` | `createStorageLocation` (frontend/src/lib/api.ts:976) | — |
| `DELETE /api/storage/locations/{id}` | `HandleDeleteStorageLocation` | `deleteStorageLocation` (frontend/src/lib/api.ts:995) | — |
| `PUT /api/storage/locations/{id}` | `HandleUpdateStorageLocation` | `updateStorageLocation` (frontend/src/lib/api.ts:986) | — |
| `GET /api/storage/status` | `(inline)` | `(module)` (frontend/src/app/page.tsx:98) | — |
| `GET /api/support/tickets` | `HandleListSupportTickets` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:85)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:67) | — |
| `POST /api/support/tickets` | `HandleCreateSupportTicket` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:134) | — |
| `GET /api/support/tickets/{id}` | `HandleGetSupportTicket` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:108)<br>`(module)` (frontend/src/components/portal/SupportWidget.tsx:180)<br>`(module)` (frontend/src/components/portal/SupportWidget.tsx:157)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:100)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:88)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:117) | — |
| `PATCH /api/support/tickets/{id}` | `HandleUpdateSupportTicket` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:153)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:131) | — |
| `POST /api/support/tickets/{id}/messages` | `HandleSupportTicketReply` | `(module)` (frontend/src/components/portal/SupportWidget.tsx:172)<br>`(module)` (frontend/src/components/reports/SupportTicketsCard.tsx:112) | — |
| `GET /api/system/health` | `HandleSystemHealth` | `getSystemHealth` (frontend/src/lib/api.ts:795) | — |
| `GET /api/system/services` | `HandleServicesHealth` | `getServicesHealth` (frontend/src/lib/api.ts:344) | — |
| `GET /api/system/services/timeseries` | `HandleAIMetricsTimeseries` | `getAIMetricsTimeseries` (frontend/src/lib/api.ts:416) | — |
| `GET /api/timeline` | `HandleGetTimeline` | `getTimeline` (frontend/src/lib/api.ts:637) | — |
| `GET /api/timeline/coverage` | `HandleGetCoverage` | `fetchCoverage` (frontend/src/lib/api.ts:744) | — |
| `GET /api/users` | `HandleListUsers` | `listUsers` (frontend/src/lib/api.ts:853) | — |
| `POST /api/users` | `HandleCreateUser` | `createUser` (frontend/src/lib/api.ts:859) | — |
| `DELETE /api/users/{id}` | `HandleDeleteUser` | `deleteUser` (frontend/src/lib/api.ts:869) | — |
| `PATCH /api/users/{id}` | `HandleUpdateUserProfile` | `updateUserProfile` (frontend/src/lib/api.ts:892) | — |
| `PATCH /api/users/{id}/password` | `HandleUpdateUserPassword` | `updateUserPassword` (frontend/src/lib/api.ts:874) | — |
| `PATCH /api/users/{id}/role` | `HandleUpdateUserRole` | `updateUserRole` (frontend/src/lib/api.ts:883) | — |
| `POST /api/v1/alarms/{alarmId}/ai-feedback` | `(inline)` | `submitAIFeedback` (frontend/src/lib/ironsight-api.ts:647) | — |
| `POST /api/v1/alarms/{alarmId}/escalate` | `HandleEscalateAlarm` | `escalateAlarm` (frontend/src/lib/ironsight-api.ts:640) | — |
| `GET /api/v1/alerts` | `(inline)` | `getAlerts` (frontend/src/lib/ironsight-api.ts:130)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `GET /api/v1/cameras` | `HandleListAllPlatformCameras` | `useMasterCameras` (frontend/src/hooks/useCameraAssignment.ts:38)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `GET /api/v1/companies` | `HandleListOrganizations` | `getCompanies` (frontend/src/lib/ironsight-api.ts:181)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `POST /api/v1/companies` | `HandleCreateOrganization` | `createCompany` (frontend/src/lib/ironsight-api.ts:189) | — |
| `GET /api/v1/companies/{companyId}/users` | `HandleListCompanyUsers` | `getCompanyUsers` (frontend/src/lib/ironsight-api.ts:198) | — |
| `POST /api/v1/companies/{companyId}/users` | `HandleCreateCompanyUser` | `createCompanyUser` (frontend/src/lib/ironsight-api.ts:202) | — |
| `GET /api/v1/detections` | `HandleListDetections` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `GET /api/v1/device-history` | `HandleGetDeviceHistory` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `GET /api/v1/dispatch/queue` | `HandleDispatchQueue` | `getAlarmQueue` (frontend/src/lib/ironsight-api.ts:548) | — |
| `GET /api/v1/events` | `HandleListSecurityEvents` | `listSecurityEvents` (frontend/src/lib/ironsight-api.ts:636)<br>`getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `POST /api/v1/events` | `HandleCreateSecurityEvent` | `createSecurityEvent` (frontend/src/lib/ironsight-api.ts:610) | — |
| `POST /api/v1/events/{id}/verify` | `HandleVerifySecurityEvent` | `verifySecurityEvent` (frontend/src/lib/ironsight-api.ts:760) | — |
| `GET /api/v1/features` | `HandleFeatureFlags` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `GET /api/v1/handoffs` | `HandleListHandoffs` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882)<br>`getPendingHandoffs` (frontend/src/lib/ironsight-api.ts:400) | — |
| `POST /api/v1/handoffs` | `HandleCreateHandoff` | `createHandoff` (frontend/src/lib/ironsight-api.ts:404) | — |
| `GET /api/v1/incidents` | `HandleListIncidents` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882)<br>`getIncidents` (frontend/src/lib/ironsight-api.ts:94) | — |
| `GET /api/v1/incidents/active` | `(inline)` | `getActiveIncidents` (frontend/src/lib/ironsight-api.ts:134)<br>`getIncident` (frontend/src/lib/ironsight-api.ts:98)<br>`getIncidentDetail` (frontend/src/lib/ironsight-api.ts:138) | — |
| `GET /api/v1/incidents/{id}` | `HandleGetIncident` | `getActiveIncidents` (frontend/src/lib/ironsight-api.ts:134)<br>`getIncident` (frontend/src/lib/ironsight-api.ts:98)<br>`getIncidentDetail` (frontend/src/lib/ironsight-api.ts:138) | — |
| `POST /api/v1/incidents/{id}/share` | `HandleCreateEvidenceShare` | `createEvidenceShareLink` (frontend/src/lib/ironsight-api.ts:816) | — |
| `GET /api/v1/incidents/{id}/shares` | `HandleListEvidenceShares` | `listIncidentShares` (frontend/src/lib/ironsight-api.ts:778) | — |
| `POST /api/v1/incidents/{incidentId}/acknowledge` | `(inline)` | `(module)` (frontend/src/components/operator/ActiveAlarmView.tsx:254) | — |
| `GET /api/v1/model-versions` | `HandleListModelVersions` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882) | — |
| `GET /api/v1/operators` | `HandleListOperators` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882)<br>`getSOCOperators` (frontend/src/lib/ironsight-api.ts:308) | — |
| `GET /api/v1/operators/current` | `HandleGetCurrentOperator` | `getCurrentOperator` (frontend/src/lib/ironsight-api.ts:322) | — |
| `GET /api/v1/portal/compliance/report.pdf` | `HandleComplianceReportPDF` | `downloadComplianceReport` (frontend/src/lib/api.ts:1335) | — |
| `GET /api/v1/portal/compliance/summary` | `HandleComplianceSummary` | `getComplianceSummary` (frontend/src/lib/api.ts:1319) | — |
| `GET /api/v1/portal/pending-review` | `HandleListPendingReview` | `getPendingReview` (frontend/src/lib/api.ts:1217) | — |
| `POST /api/v1/portal/pending-review/{id}/review` | `HandleReviewPendingEntry` | `submitReview` (frontend/src/lib/api.ts:1226) | — |
| `GET /api/v1/portal/summary` | `HandlePortalSummary` | `getPortalSummary` (frontend/src/lib/ironsight-api.ts:118) | — |
| `DELETE /api/v1/shares/{token}` | `HandleRevokeEvidenceShare` | `revokeEvidenceShareLink` (frontend/src/lib/ironsight-api.ts:835) | — |
| `GET /api/v1/sites` | `HandleListSites` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882)<br>`getSites` (frontend/src/lib/ironsight-api.ts:69) | — |
| `POST /api/v1/sites` | `HandleCreateSiteP` | `createSite` (frontend/src/lib/ironsight-api.ts:211) | — |
| `GET /api/v1/sites/locks` | `HandleSiteLocks` | `getSiteLocks` (frontend/src/lib/ironsight-api.ts:332)<br>`getSite` (frontend/src/lib/ironsight-api.ts:73) | — |
| `DELETE /api/v1/sites/{id}` | `HandleDeleteSiteP` | `deleteSite` (frontend/src/lib/ironsight-api.ts:225) | — |
| `GET /api/v1/sites/{id}` | `HandleGetSite` | `getSiteLocks` (frontend/src/lib/ironsight-api.ts:332)<br>`getSite` (frontend/src/lib/ironsight-api.ts:73) | — |
| `PUT /api/v1/sites/{id}` | `HandleUpdateSite` | `updateSite` (frontend/src/lib/ironsight-api.ts:218) | — |
| `GET /api/v1/sites/{id}/contacts` | `HandleListSiteContacts` | `(module)` (frontend/src/app/portal/sites/[id]/contacts/page.tsx:42) | — |
| `PUT /api/v1/sites/{id}/contacts` | `HandleUpdateSiteContacts` | `(module)` (frontend/src/app/portal/sites/[id]/contacts/page.tsx:51) | — |
| `POST /api/v1/sites/{siteId}/camera-assignments` | `HandleAssignCamera` | `assignCameraToSite` (frontend/src/lib/ironsight-api.ts:235) | — |
| `DELETE /api/v1/sites/{siteId}/camera-assignments/{cameraId}` | `HandleUnassignCamera` | `unassignCamera` (frontend/src/lib/ironsight-api.ts:242) | — |
| `GET /api/v1/sites/{siteId}/cameras` | `HandleGetSiteCameras` | `getSiteCameras` (frontend/src/lib/ironsight-api.ts:77) | — |
| `GET /api/v1/sites/{siteId}/sops` | `HandleListSiteSOPs` | `getSiteSOPs` (frontend/src/lib/ironsight-api.ts:359) | — |
| `POST /api/v1/sites/{siteId}/sops` | `HandleCreateSiteSOP` | `createSiteSOP` (frontend/src/lib/ironsight-api.ts:363) | — |
| `POST /api/v1/sites/{siteId}/speaker-assignments` | `HandleAssignSpeaker` | `assignSpeakerToSite` (frontend/src/lib/ironsight-api.ts:252) | — |
| `DELETE /api/v1/sites/{siteId}/speaker-assignments/{speakerId}` | `HandleUnassignSpeaker` | `unassignSpeaker` (frontend/src/lib/ironsight-api.ts:259) | — |
| `DELETE /api/v1/sops/{id}` | `HandleDeleteSiteSOP` | `deleteSiteSOP` (frontend/src/lib/ironsight-api.ts:377) | — |
| `PUT /api/v1/sops/{id}` | `HandleUpdateSiteSOP` | `updateSiteSOP` (frontend/src/lib/ironsight-api.ts:370) | — |
| `GET /api/v1/speakers` | `HandleListAllPlatformSpeakers` | `getFeatureFlags` (frontend/src/lib/ironsight-api.ts:882)<br>`getAllSpeakers` (frontend/src/lib/ironsight-api.ts:248) | — |
| `POST /auth/login` | `HandleLogin` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:172) | — |
| `POST /auth/logout` | `HandleLogout` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:206) | — |
| `GET /auth/me` | `HandleGetMe` | `AuthProvider` (frontend/src/contexts/AuthContext.tsx:133) | — |

## B. Backend-only routes

Routes with no frontend caller. Annotated routes are called by external clients by design; unannotated rows are either unwired backend surface or callers the static scan cannot resolve.

| Route | Handler | External caller | Features |
|---|---|---|---|
| `GET /api/admin/labeling/export` | `HandleExportLabeledDataset` | — | — |
| `GET /api/admin/labeling/jobs` | `HandleListLabelJobs` | — | — |
| `POST /api/admin/labeling/jobs/next` | `HandleClaimNextLabelJob` | — | — |
| `POST /api/admin/labeling/jobs/{id}/claim` | `HandleClaimLabelJob` | — | — |
| `POST /api/admin/labeling/jobs/{id}/label` | `HandleSubmitLabel` | — | — |
| `GET /api/admin/labeling/stats` | `HandleLabelingStats` | — | — |
| `POST /api/admin/reanalyze/` | `HandleAdminReanalyze` | — | — |
| `GET /api/admin/reanalyze/{run_id}` | `HandleGetReanalyzeRun` | — | — |
| `GET /api/audio-messages/file/{fileName}` | `HandleServeAudioFile` | — | — |
| `GET /api/audio-messages/{id}` | `HandleGetAudioMessage` | — | — |
| `POST /api/auth/mfa/confirm` | `HandleMFAConfirm` | — | — |
| `POST /api/auth/mfa/disable` | `HandleMFADisable` | — | — |
| `POST /api/auth/mfa/enroll` | `HandleMFAEnroll` | — | — |
| `GET /api/cameras/{id}/detect/stream` | `HandleDetectionStream` | — | — |
| `GET /api/cameras/{id}/recordings` | `HandleGetRecordings` | — | — |
| `GET /api/events/{id}/export` | `HandleEvidenceExport` | — | — |
| `POST /api/integrations/milesight/sense/{token}` | `HandleSenseWebhook` | camera push webhook (token-auth) | — |
| `GET /api/live/{cameraID}/*` | `HandleLiveProxy` | — | — |
| `GET /api/playback/{id}/playlist.m3u8` | `HandlePlaybackHLS` | — | — |
| `GET /api/reports/sla` | `HandleSLAReport` | — | — |
| `GET /api/system/services/usage` | `HandleAIUsageBySite` | — | — |
| `POST /api/users/{id}/mfa/reset` | `HandleAdminMFAReset` | — | — |
| `DELETE /api/v1/companies/{companyId}/users/{userId}` | `HandleDeleteCompanyUser` | — | — |
| `DELETE /api/v1/companies/{id}` | `HandleDeleteOrganization` | — | — |
| `PUT /api/v1/companies/{id}` | `HandleUpdateOrganization` | — | — |
| `GET /api/v1/evidence/manifests` | `HandleListManifests` | — | — |
| `GET /api/v1/evidence/manifests/{id}` | `HandleGetManifest` | — | — |
| `GET /api/v1/evidence/manifests/{id}/verify` | `HandleVerifyManifest` | — | — |
| `POST /api/v1/operators` | `HandleCreateOperator` | — | — |
| `GET /api/v1/operators/{operatorId}/handoffs` | `HandleOperatorHandoffs` | — | — |
| `GET /api/v1/portal/pending-review/{id}/frame` | `HandleServePPEFrame` | — | — |
| `GET /api/v1/portal/person-tracks` | `HandleGetPersonTracks` | — | — |
| `PUT /api/v1/sites/{id}/monitoring-schedule` | `HandleUpdateSiteMonitoringSchedule` | — | — |
| `PATCH /api/v1/sites/{id}/recording` | `HandleUpdateSiteRecording` | — | — |
| `ANY /exports/*` | `http.StripPrefix` | evidence ZIP download links | — |
| `GET /media/v1/{token}` | `HandleMediaServe` | minted media URLs consumed via <img>/<video> src | — |
| `GET /metrics` | `metricsHandler.ServeHTTP` | Prometheus scraper (network-trust) | — |
| `GET /metrics` | `metricsHandler.ServeHTTP` | Prometheus scraper (network-trust) | — |
| `GET /share/{token}` | `HandlePublicEvidenceShare` | public evidence share links (token-auth) | — |
| `GET /ws` | `hub.HandleWebSocket` | — | — |
| `GET /ws/alerts` | `hub.HandleWebSocket` | — | — |

## C. Frontend-only calls

These call sites match no registered route — each one 404s at runtime and needs a fix or removal.

| Call | Where |
|---|---|
| `GET /api/cameras/{*}/compliance-rules` | `listComplianceRules` (frontend/src/lib/api.ts:1270) |
| `POST /api/cameras/{*}/compliance-rules` | `createComplianceRule` (frontend/src/lib/api.ts:1276) |
| `PUT /api/cameras/{*}/compliance-rules/{*}` | `updateComplianceRule` (frontend/src/lib/api.ts:1286) |
| `DELETE /api/cameras/{*}/compliance-rules/{*}` | `deleteComplianceRule` (frontend/src/lib/api.ts:1295) |
| `GET /api/cameras/{*}/ppe/zones` | `listPPEZones` (frontend/src/lib/api.ts:1240) |
| `POST /api/cameras/{*}/ppe/zones` | `createPPEZone` (frontend/src/lib/api.ts:1246) |
| `PUT /api/cameras/{*}/ppe/zones/{*}` | `updatePPEZone` (frontend/src/lib/api.ts:1256) |
| `DELETE /api/cameras/{*}/ppe/zones/{*}` | `deletePPEZone` (frontend/src/lib/api.ts:1265) |
| `PUT /api/sites/{*}/monitoring-schedule` | `useUpdateSiteMonitoringSchedule` (frontend/src/hooks/useSites.ts:75) |
| `POST /api/v1/ai-telemetry/corrections` | `submitAICorrection` (frontend/src/lib/ironsight-api.ts:860) |
| `DELETE /api/v1/alerts/{*}/claim` | `releaseAlert` (frontend/src/lib/ironsight-api.ts:353) |
| `PUT /api/v1/alerts/{*}/claim` | `claimAlert` (frontend/src/lib/ironsight-api.ts:346) |
| `POST /api/v1/audit` | `logAuditAction` (frontend/src/lib/ironsight-api.ts:418) |
| `GET /api/v1/audit` | `getAuditLog` (frontend/src/lib/ironsight-api.ts:414) |
| `POST /api/v1/cameras` | `useCreateCamera` (frontend/src/hooks/useCameraAssignment.ts:104) |
| `DELETE /api/v1/cameras/{*}` | `useDeleteCamera` (frontend/src/hooks/useCameraAssignment.ts:122) |
| `GET /api/v1/cameras/{*}/ptz` | `getPTZCapability` (frontend/src/lib/ironsight-api.ts:483) |
| `POST /api/v1/cameras/{*}/ptz/command` | `sendPTZCommand` (frontend/src/lib/ironsight-api.ts:487) |
| `GET /api/v1/companies/{*}` | `getCompany` (frontend/src/lib/ironsight-api.ts:185) |
| `PUT /api/v1/handoffs/{*}/accept` | `acceptHandoff` (frontend/src/lib/ironsight-api.ts:408) |
| `POST /api/v1/incidents/{*}/comments` | `addIncidentComment` (frontend/src/lib/ironsight-api.ts:109) |
| `POST /api/v1/incidents/{*}/evidence` | `generateEvidencePackage` (frontend/src/lib/ironsight-api.ts:460) |
| `PUT /api/v1/incidents/{*}/status` | `updateIncidentStatus` (frontend/src/lib/ironsight-api.ts:102) |
| `GET /api/v1/integrations` | `getIntegrations` (frontend/src/lib/ironsight-api.ts:521) |
| `POST /api/v1/integrations` | `createIntegration` (frontend/src/lib/ironsight-api.ts:525) |
| `PATCH /api/v1/integrations/{*}` | `toggleIntegration` (frontend/src/lib/ironsight-api.ts:529) |
| `DELETE /api/v1/integrations/{*}` | `deleteIntegration` (frontend/src/lib/ironsight-api.ts:533) |
| `DELETE /api/v1/notifications/{*}` | `deleteNotificationRule` (frontend/src/lib/ironsight-api.ts:477) |
| `GET /api/v1/operators/metrics` | `getOperatorMetrics` (frontend/src/lib/ironsight-api.ts:440) |
| `PUT /api/v1/operators/presence` | `updatePresence` (frontend/src/lib/ironsight-api.ts:428) |
| `GET /api/v1/operators/presence` | `getOperatorPresence` (frontend/src/lib/ironsight-api.ts:424) |
| `PUT /api/v1/operators/{*}/presence` | `updateOperatorPresence` (frontend/src/lib/ironsight-api.ts:541) |
| `POST /api/v1/reports/generate` | `generateReport` (frontend/src/lib/ironsight-api.ts:172) |
| `GET /api/v1/reports/scheduled` | `getScheduledReports` (frontend/src/lib/ironsight-api.ts:446) |
| `POST /api/v1/reports/scheduled` | `createScheduledReport` (frontend/src/lib/ironsight-api.ts:450) |
| `PATCH /api/v1/reports/scheduled/{*}` | `toggleScheduledReport` (frontend/src/lib/ironsight-api.ts:454) |
| `GET /api/v1/reports/sla` | `getSLAReport` (frontend/src/lib/ironsight-api.ts:688) |
| `GET /api/v1/safety/findings/pending{*}` | `getPendingSafetyFindings` (frontend/src/lib/ironsight-api.ts:842) |
| `POST /api/v1/safety/findings/{*}/validate` | `validateSafetyFinding` (frontend/src/lib/ironsight-api.ts:846) |
| `POST /api/v1/search/saved` | `createSavedSearch` (frontend/src/lib/ironsight-api.ts:511) |
| `GET /api/v1/search/saved` | `getSavedSearches` (frontend/src/lib/ironsight-api.ts:507) |
| `DELETE /api/v1/search/saved/{*}` | `deleteSavedSearch` (frontend/src/lib/ironsight-api.ts:515) |
| `GET /api/v1/search/suggest` | `getSearchSuggestions` (frontend/src/lib/ironsight-api.ts:163) |
| `GET /api/v1/sites/{*}/camera-assignments` | `getCameraAssignments` (frontend/src/lib/ironsight-api.ts:231) |
| `GET /api/v1/sites/{*}/compliance` | `getSiteCompliance` (frontend/src/lib/ironsight-api.ts:81) |
| `DELETE /api/v1/sites/{*}/lock` | `unlockSite` (frontend/src/lib/ironsight-api.ts:340) |
| `POST /api/v1/sites/{*}/lock` | `lockSite` (frontend/src/lib/ironsight-api.ts:336) |
| `GET /api/v1/sites/{*}/map` | `getSiteMap` (frontend/src/lib/ironsight-api.ts:383) |
| `PUT /api/v1/sites/{*}/map` | `updateSiteMap` (frontend/src/lib/ironsight-api.ts:387) |
| `GET /api/v1/sites/{*}/notifications` | `getNotificationRules` (frontend/src/lib/ironsight-api.ts:469) |
| `POST /api/v1/sites/{*}/notifications` | `createNotificationRule` (frontend/src/lib/ironsight-api.ts:473) |
| `GET /api/v1/sites/{*}/users` | `getSiteUsers` (frontend/src/lib/ironsight-api.ts:265) |
| `POST /api/v1/sites/{*}/users` | `assignUserToSite` (frontend/src/lib/ironsight-api.ts:269) |
| `DELETE /api/v1/sites/{*}/users/{*}` | `unassignUserFromSite` (frontend/src/lib/ironsight-api.ts:276) |
| `GET /api/v1/sites/{*}/zones` | `getExclusionZones` (frontend/src/lib/ironsight-api.ts:493) |
| `POST /api/v1/sites/{*}/zones` | `createExclusionZone` (frontend/src/lib/ironsight-api.ts:497) |
| `GET /api/v1/sla` | `getSLAConfigs` (frontend/src/lib/ironsight-api.ts:434) |
| `DELETE /api/v1/zones/{*}` | `deleteExclusionZone` (frontend/src/lib/ironsight-api.ts:501) |
