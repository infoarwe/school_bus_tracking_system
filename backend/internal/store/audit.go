package store

import (
	"context"
	"encoding/json"
	"fmt"
)

type AuditEntry struct {
	SchoolID    *string
	ActorUserID *string
	ActorRole   string
	Action      string // e.g. "school.create", "user.suspend"
	EntityType  string
	EntityID    *string
	Before      any
	After       any
	IP          string
	RequestID   string
}

func InsertAudit(ctx context.Context, q DBTX, e AuditEntry) error {
	before, err := toJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := toJSON(e.After)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO audit_logs (school_id, actor_user_id, actor_role, action, entity_type, entity_id,
			before, after, ip, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.SchoolID, e.ActorUserID, e.ActorRole, e.Action, e.EntityType, e.EntityID,
		before, after, e.IP, e.RequestID)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
