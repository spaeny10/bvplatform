package api

// rls_middleware.go — P4-SCHEMA-07: per-request tenant GUC setter.
//
// RLSMiddleware is mounted INSIDE the RequireAuth group (after claims are
// resolved) for routes that serve customer-org data. It does NOT acquire
// a connection from the pool — connection acquisition is expensive and
// should happen lazily in the handler. Instead it stores the tenant ID in
// the request context so individual handlers can call
// db.AcquireWithTenant(ctx, tenant) when they want RLS enforcement.
//
// Two categories of routes are covered:
//   A. Routes under /api where the caller is a customer-org user
//      (claims.OrganizationID != "") — the tenant is claims.OrganizationID.
//   B. SOC/admin routes — claims.OrganizationID == "". These callers connect
//      as 'onvif' which has the service_bypass policy, so app_current_tenant()
//      returning NULL is correct (all rows visible to bypass-role connections).
//      The middleware stores "" so handlers can detect the service-mode path.
//
// The middleware does NOT set a GUC on the shared pool. Setting a GUC on a
// pool connection would require acquiring a connection and holding it for the
// entire request lifetime, which defeats the pool's purpose. The lazy
// AcquireWithTenant pattern is the correct design.
//
// Usage in handlers:
//
//   tenant := TenantFromContext(r.Context())
//   if tenant != "" {
//       conn, tx, err := db.AcquireWithTenant(r.Context(), db.Pool, tenant)
//       if err != nil { ... }
//       defer conn.Release()
//       defer tx.Rollback(r.Context())
//       // queries via tx.Query/Exec ...
//       tx.Commit(r.Context())
//   } else {
//       // SOC/service role — use db.Pool directly (service_bypass policy applies)
//   }

import (
	"context"
	"net/http"
)

// contextKeyTenant is the context key for the per-request tenant ID.
// Distinct from ContextKeyClaims to avoid any confusion.
type contextKeyTenant struct{}

// RLSMiddleware extracts the tenant org ID from the request's JWT claims
// (which RequireAuth must have already resolved) and stores it in the
// request context. Handlers downstream can retrieve it with TenantFromContext.
//
// The middleware must be mounted INSIDE the RequireAuth group.
func RLSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := ""
		if claims := claimsFromRequest(r); claims != nil {
			tenant = claims.OrganizationID
		}
		ctx := context.WithValue(r.Context(), contextKeyTenant{}, tenant)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TenantFromContext returns the tenant org ID stored by RLSMiddleware.
// Returns "" for SOC/admin roles or unauthenticated requests.
func TenantFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contextKeyTenant{}).(string); ok {
		return v
	}
	return ""
}
