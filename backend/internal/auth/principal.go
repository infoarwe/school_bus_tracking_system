package auth

import (
	"context"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// Principal is the authenticated caller, set on the request context by the auth middleware.
type Principal struct {
	UserID      string
	Role        models.Role
	SchoolID    *string // nil only for Super Admin
	SessionID   string
	TOTPEnabled bool
}

func (p *Principal) IsSuperAdmin() bool { return p.Role == models.RoleSuperAdmin }

// CanAccessSchool is the tenant isolation check: Super Admin sees every school,
// everyone else only their own.
func (p *Principal) CanAccessSchool(schoolID string) bool {
	return p.IsSuperAdmin() || (p.SchoolID != nil && *p.SchoolID == schoolID)
}

func (p *Principal) HasRole(roles ...models.Role) bool {
	for _, r := range roles {
		if p.Role == r {
			return true
		}
	}
	return false
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the caller, or nil on unauthenticated routes.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
