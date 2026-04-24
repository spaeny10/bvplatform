'use client';

import { createContext, useContext, useState, useCallback, useEffect, ReactNode } from 'react';

export type Locale = 'en' | 'es' | 'fr';

export const LOCALE_INFO: Record<Locale, { label: string; flag: string; dir: 'ltr' | 'rtl' }> = {
  en: { label: 'English', flag: '🇺🇸', dir: 'ltr' },
  es: { label: 'Español', flag: '🇪🇸', dir: 'ltr' },
  fr: { label: 'Français', flag: '🇫🇷', dir: 'ltr' },
};

// ── Translation Dictionaries ──
const translations: Record<Locale, Record<string, string>> = {
  en: {
    // Navigation
    'nav.monitor': 'Monitor',
    'nav.portal': 'Portal',
    'nav.search': 'Search',
    'nav.analytics': 'Analytics',
    'nav.admin': 'Admin',
    // Operator
    'op.fleet_overview': 'Fleet Overview',
    'op.sites': 'Sites',
    'op.cameras': 'Cameras',
    'op.workers': 'Workers',
    'op.incidents': 'Open Incidents',
    'op.active': 'Active',
    'op.online': 'Online',
    'op.on_site': 'On Site',
    'op.lock_site': 'Lock Site',
    'op.unlock_site': 'Release Lock',
    'op.alerts': 'Alerts',
    'op.all': 'All',
    'op.critical': 'Critical',
    'op.high': 'High',
    'op.medium': 'Medium',
    'op.low': 'Low',
    'op.claim': 'Claim',
    'op.handoff': 'Handoff',
    'op.multi_site': 'Multi-Site',
    'op.layout': 'Layout',
    // Portal
    'portal.dashboard': 'Safety Dashboard',
    'portal.compliance': 'Compliance Score',
    'portal.incidents': 'Active Incidents',
    'portal.workers': 'Workers on Site',
    'portal.cameras': 'Camera Health',
    'portal.export': 'Export',
    'portal.generate_report': 'Generate Report',
    // Admin
    'admin.title': 'Admin',
    'admin.companies': 'Companies',
    'admin.create_site': 'Create Site',
    'admin.audit_trail': 'Audit Trail',
    'admin.export_csv': 'Export CSV',
    'admin.export_json': 'Export JSON',
    // Common
    'common.close': 'Close',
    'common.save': 'Save',
    'common.cancel': 'Cancel',
    'common.loading': 'Loading...',
    'common.error': 'Error',
    'common.retry': 'Retry',
    'common.search': 'Search',
    'common.filter': 'Filter',
    'common.reset': 'Reset',
    'common.today': 'Today',
    'common.last_7d': 'Last 7 days',
    'common.last_30d': 'Last 30 days',
    // Session
    'session.expiring': 'Session Expiring Soon',
    'session.extend': 'Extend Session',
    'session.logout': 'Log Out',
    // Accessibility
    'a11y.skip_to_content': 'Skip to main content',
    'a11y.menu_open': 'Open navigation menu',
    'a11y.menu_close': 'Close navigation menu',
  },
  es: {
    'nav.monitor': 'Monitoreo',
    'nav.portal': 'Portal',
    'nav.search': 'Buscar',
    'nav.analytics': 'Analítica',
    'nav.admin': 'Admin',
    'op.fleet_overview': 'Vista General',
    'op.sites': 'Sitios',
    'op.cameras': 'Cámaras',
    'op.workers': 'Trabajadores',
    'op.incidents': 'Incidentes Abiertos',
    'op.active': 'Activos',
    'op.online': 'En Línea',
    'op.on_site': 'En Sitio',
    'op.lock_site': 'Bloquear Sitio',
    'op.unlock_site': 'Liberar Bloqueo',
    'op.alerts': 'Alertas',
    'op.all': 'Todos',
    'op.critical': 'Crítico',
    'op.high': 'Alto',
    'op.medium': 'Medio',
    'op.low': 'Bajo',
    'op.claim': 'Reclamar',
    'op.handoff': 'Traspaso',
    'op.multi_site': 'Multi-Sitio',
    'op.layout': 'Diseño',
    'portal.dashboard': 'Panel de Seguridad',
    'portal.compliance': 'Cumplimiento',
    'portal.incidents': 'Incidentes Activos',
    'portal.workers': 'Trabajadores en Sitio',
    'portal.cameras': 'Estado de Cámaras',
    'portal.export': 'Exportar',
    'portal.generate_report': 'Generar Informe',
    'admin.title': 'Administración',
    'admin.companies': 'Empresas',
    'admin.create_site': 'Crear Sitio',
    'admin.audit_trail': 'Registro de Auditoría',
    'admin.export_csv': 'Exportar CSV',
    'admin.export_json': 'Exportar JSON',
    'common.close': 'Cerrar',
    'common.save': 'Guardar',
    'common.cancel': 'Cancelar',
    'common.loading': 'Cargando...',
    'common.error': 'Error',
    'common.retry': 'Reintentar',
    'common.search': 'Buscar',
    'common.filter': 'Filtrar',
    'common.reset': 'Restablecer',
    'common.today': 'Hoy',
    'common.last_7d': 'Últimos 7 días',
    'common.last_30d': 'Últimos 30 días',
    'session.expiring': 'Sesión Expirando',
    'session.extend': 'Extender Sesión',
    'session.logout': 'Cerrar Sesión',
    'a11y.skip_to_content': 'Ir al contenido principal',
    'a11y.menu_open': 'Abrir menú',
    'a11y.menu_close': 'Cerrar menú',
  },
  fr: {
    'nav.monitor': 'Surveillance',
    'nav.portal': 'Portail',
    'nav.search': 'Recherche',
    'nav.analytics': 'Analytique',
    'nav.admin': 'Admin',
    'op.fleet_overview': 'Vue d\'ensemble',
    'op.sites': 'Sites',
    'op.cameras': 'Caméras',
    'op.workers': 'Travailleurs',
    'op.incidents': 'Incidents Ouverts',
    'op.active': 'Actifs',
    'op.online': 'En Ligne',
    'op.on_site': 'Sur Site',
    'op.lock_site': 'Verrouiller le Site',
    'op.unlock_site': 'Déverrouiller',
    'op.alerts': 'Alertes',
    'op.all': 'Tous',
    'op.critical': 'Critique',
    'op.high': 'Haut',
    'op.medium': 'Moyen',
    'op.low': 'Bas',
    'op.claim': 'Réclamer',
    'op.handoff': 'Transfert',
    'op.multi_site': 'Multi-Site',
    'op.layout': 'Disposition',
    'portal.dashboard': 'Tableau de Sécurité',
    'portal.compliance': 'Conformité',
    'portal.incidents': 'Incidents Actifs',
    'portal.workers': 'Travailleurs sur Site',
    'portal.cameras': 'État des Caméras',
    'portal.export': 'Exporter',
    'portal.generate_report': 'Générer un Rapport',
    'admin.title': 'Administration',
    'admin.companies': 'Entreprises',
    'admin.create_site': 'Créer un Site',
    'admin.audit_trail': 'Journal d\'Audit',
    'admin.export_csv': 'Exporter CSV',
    'admin.export_json': 'Exporter JSON',
    'common.close': 'Fermer',
    'common.save': 'Sauver',
    'common.cancel': 'Annuler',
    'common.loading': 'Chargement...',
    'common.error': 'Erreur',
    'common.retry': 'Réessayer',
    'common.search': 'Rechercher',
    'common.filter': 'Filtrer',
    'common.reset': 'Réinitialiser',
    'common.today': 'Aujourd\'hui',
    'common.last_7d': '7 derniers jours',
    'common.last_30d': '30 derniers jours',
    'session.expiring': 'Session Expirante',
    'session.extend': 'Prolonger la Session',
    'session.logout': 'Déconnexion',
    'a11y.skip_to_content': 'Aller au contenu principal',
    'a11y.menu_open': 'Ouvrir le menu',
    'a11y.menu_close': 'Fermer le menu',
  },
};

// ── Context ──
interface I18nContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
  dir: 'ltr' | 'rtl';
}

const I18nContext = createContext<I18nContextValue>({
  locale: 'en',
  setLocale: () => {},
  t: (key) => key,
  dir: 'ltr',
});

export function useI18n() {
  return useContext(I18nContext);
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>('en');

  useEffect(() => {
    const saved = localStorage.getItem('sg-locale') as Locale | null;
    if (saved && translations[saved]) {
      setLocaleState(saved);
      document.documentElement.lang = saved;
      document.documentElement.dir = LOCALE_INFO[saved].dir;
    }
  }, []);

  const setLocale = useCallback((newLocale: Locale) => {
    setLocaleState(newLocale);
    localStorage.setItem('sg-locale', newLocale);
    document.documentElement.lang = newLocale;
    document.documentElement.dir = LOCALE_INFO[newLocale].dir;
  }, []);

  const t = useCallback((key: string, params?: Record<string, string | number>): string => {
    let str = translations[locale]?.[key] || translations['en']?.[key] || key;
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        str = str.replace(`{${k}}`, String(v));
      });
    }
    return str;
  }, [locale]);

  return (
    <I18nContext.Provider value={{ locale, setLocale, t, dir: LOCALE_INFO[locale].dir }}>
      {children}
    </I18nContext.Provider>
  );
}

// ── Language Switcher Component ──
export function LanguageSwitcher() {
  const { locale, setLocale } = useI18n();
  const [open, setOpen] = useState(false);

  return (
    <div style={{ position: 'relative' }}>
      <button
        onClick={() => setOpen(v => !v)}
        aria-label="Change language"
        style={{
          padding: '4px 8px', borderRadius: 4,
          background: 'rgba(255,255,255,0.03)',
          border: '1px solid rgba(255,255,255,0.06)',
          color: '#8891A5', fontSize: 11, cursor: 'pointer',
          fontFamily: "'JetBrains Mono', monospace",
          display: 'flex', alignItems: 'center', gap: 4,
        }}
      >
        {LOCALE_INFO[locale].flag} {locale.toUpperCase()}
      </button>

      {open && (
        <div style={{
          position: 'absolute', top: '100%', right: 0, marginTop: 4,
          background: '#0E1117', border: '1px solid rgba(255,255,255,0.08)',
          borderRadius: 6, padding: 4, minWidth: 130,
          boxShadow: '0 8px 24px rgba(0,0,0,0.4)',
          zIndex: 500,
        }}>
          {(Object.entries(LOCALE_INFO) as [Locale, typeof LOCALE_INFO[Locale]][]).map(([key, info]) => (
            <button
              key={key}
              onClick={() => { setLocale(key); setOpen(false); }}
              style={{
                display: 'flex', alignItems: 'center', gap: 8,
                width: '100%', padding: '6px 8px', borderRadius: 3,
                background: locale === key ? 'rgba(0,212,255,0.06)' : 'transparent',
                border: locale === key ? '1px solid rgba(0,212,255,0.15)' : '1px solid transparent',
                color: locale === key ? '#E8732A' : '#8891A5',
                cursor: 'pointer', fontSize: 11, textAlign: 'left',
                fontFamily: 'inherit',
              }}
            >
              <span style={{ fontSize: 14 }}>{info.flag}</span>
              {info.label}
              {locale === key && <span style={{ marginLeft: 'auto', fontSize: 8, color: '#E8732A' }}>●</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
