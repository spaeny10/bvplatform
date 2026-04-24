// ── Admin Store ──
// UI state for site/customer/camera/SOP/map/notification management workflows.

import { create } from 'zustand';

interface AdminState {
  // Modals
  showCreateSiteModal: boolean;
  showAssignCameraModal: boolean;
  showCustomerAccessModal: boolean;
  showCreateCustomerModal: boolean;
  showSOPModal: boolean;
  showSiteMapModal: boolean;
  showNotificationModal: boolean;

  // Context
  editingSiteId: string | null;

  // Actions
  openCreateSite: () => void;
  openEditSite: (siteId: string) => void;
  openAssignCamera: (siteId: string) => void;
  openCustomerAccess: (siteId: string) => void;
  openCreateCustomer: () => void;
  openSOPs: (siteId: string) => void;
  openSiteMap: (siteId: string) => void;
  openNotifications: (siteId: string) => void;
  closeModals: () => void;
}

export const useAdminStore = create<AdminState>((set) => ({
  showCreateSiteModal: false,
  showAssignCameraModal: false,
  showCustomerAccessModal: false,
  showCreateCustomerModal: false,
  showSOPModal: false,
  showSiteMapModal: false,
  showNotificationModal: false,
  editingSiteId: null,

  openCreateSite: () =>
    set({ showCreateSiteModal: true, editingSiteId: null }),
  openEditSite: (siteId) =>
    set({ showCreateSiteModal: true, editingSiteId: siteId }),
  openAssignCamera: (siteId) =>
    set({ showAssignCameraModal: true, editingSiteId: siteId }),
  openCustomerAccess: (siteId) =>
    set({ showCustomerAccessModal: true, editingSiteId: siteId }),
  openCreateCustomer: () =>
    set({ showCreateCustomerModal: true }),
  openSOPs: (siteId) =>
    set({ showSOPModal: true, editingSiteId: siteId }),
  openSiteMap: (siteId) =>
    set({ showSiteMapModal: true, editingSiteId: siteId }),
  openNotifications: (siteId) =>
    set({ showNotificationModal: true, editingSiteId: siteId }),
  closeModals: () =>
    set({
      showCreateSiteModal: false,
      showAssignCameraModal: false,
      showCustomerAccessModal: false,
      showCreateCustomerModal: false,
      showSOPModal: false,
      showSiteMapModal: false,
      showNotificationModal: false,
      editingSiteId: null,
    }),
}));
