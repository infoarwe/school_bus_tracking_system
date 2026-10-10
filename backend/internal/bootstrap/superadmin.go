// Package bootstrap creates the first Super Admin on an empty database (any
// environment, including production). It is used by `cmd/admin create-super-admin`.
package bootstrap

import (
	"context"
	"errors"
	"strings"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

var (
	// ErrSuperAdminExists: bootstrap only works while there is no Super Admin at all.
	ErrSuperAdminExists = errors.New("a Super Admin already exists; this command only creates the first one")
	ErrEmailTaken       = errors.New("this email is already used by another account")
)

// InvalidInput lists per-field problems (same rules as the API).
type InvalidInput struct{ Fields validate.Errors }

func (e *InvalidInput) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for f, msg := range e.Fields {
		parts = append(parts, f+": "+msg)
	}
	return "invalid input: " + strings.Join(parts, "; ")
}

// CreateFirstSuperAdmin creates the Super Admin and audits it, in one
// transaction. 2FA is not set up here: with REQUIRE_SUPER_ADMIN_2FA (forced in
// production) the API makes the new admin set it up at first login.
func CreateFirstSuperAdmin(ctx context.Context, st *store.Store, name, email, password string) (*models.User, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	v := validate.New()
	v.Required("name", name)
	v.MaxLen("name", name, 200)
	v.Required("email", email)
	v.Email("email", email)
	if msg := auth.PasswordProblem(password); msg != "" {
		v.Check(false, "password", msg)
	}
	if !v.OK() {
		return nil, &InvalidInput{Fields: v.Errors()}
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}

	var u *models.User
	err = st.InTx(ctx, func(q store.DBTX) error {
		// Serialise concurrent bootstraps so two runs cannot both see "no Super Admin".
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('sbts.bootstrap_super_admin'))`); err != nil {
			return err
		}
		exists, err := store.SuperAdminExists(ctx, q)
		if err != nil {
			return err
		}
		if exists {
			return ErrSuperAdminExists
		}
		if _, err := store.GetUserByEmail(ctx, q, email); err == nil {
			return ErrEmailTaken
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if u, err = store.CreateUser(ctx, q, store.UserInput{
			Role: models.RoleSuperAdmin, Name: name, Email: &email, PasswordHash: &hash,
		}); err != nil {
			return err
		}
		return store.InsertAudit(ctx, q, store.AuditEntry{
			ActorRole: "system", Action: "user.bootstrap_super_admin", EntityType: "user", EntityID: &u.ID,
			After: map[string]any{"user": u, "source": "cli: admin create-super-admin"},
		})
	})
	var ce *store.ConflictError
	if errors.As(err, &ce) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}
