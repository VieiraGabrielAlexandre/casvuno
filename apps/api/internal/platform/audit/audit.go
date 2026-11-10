// Package audit records append-only audit events in the caller's transaction (ADR 0012).
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit/store"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Event describes one audited change. Metadata must never contain secrets or tokens.
type Event struct {
	HouseholdID uuid.UUID
	ActorUserID uuid.UUID
	EntityType  string
	EntityID    uuid.UUID
	Action      string
	Metadata    map[string]any
}

// Recorder writes audit events.
type Recorder struct {
	tx *postgres.TxManager
}

// NewRecorder returns a Recorder that writes through tx (joining any open transaction).
func NewRecorder(tx *postgres.TxManager) *Recorder { return &Recorder{tx: tx} }

// Record stores e. Call it inside the same WithinTx as the audited change.
func (r *Recorder) Record(ctx context.Context, e Event) error {
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate audit id: %w", err)
	}
	err = store.New(r.tx.Conn(ctx)).InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ID:          id,
		HouseholdID: e.HouseholdID,
		ActorUserID: e.ActorUserID,
		EntityType:  e.EntityType,
		EntityID:    e.EntityID,
		Action:      e.Action,
		Metadata:    raw,
	})
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}
