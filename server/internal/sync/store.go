package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Change struct {
	ID        int64           `json:"id"`
	Entity    string          `json:"entity"`
	EntityID  string          `json:"entityId"`
	Operation string          `json:"operation"`
	Version   int64           `json:"version"`
	Data      json.RawMessage `json:"data"`
	CreatedAt int64           `json:"createdAt"`
}

type Store struct{ DB *sql.DB }

func (s Store) Changes(userID string, cursor int64, limit int) ([]Change, int64, error) {
	rows, err := s.DB.Query(`SELECT id, entity_type, entity_id, operation, version, payload, created_at FROM sync_changes WHERE user_id = ? AND id > ? ORDER BY id LIMIT ?`, userID, cursor, limit)
	if err != nil {
		return nil, cursor, fmt.Errorf("query sync changes: %w", err)
	}
	defer rows.Close()
	result := []Change{}
	next := cursor
	for rows.Next() {
		var item Change
		if err := rows.Scan(&item.ID, &item.Entity, &item.EntityID, &item.Operation, &item.Version, &item.Data, &item.CreatedAt); err != nil {
			return nil, cursor, fmt.Errorf("scan sync change: %w", err)
		}
		result = append(result, item)
		if item.ID > next {
			next = item.ID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, cursor, fmt.Errorf("iterate sync changes: %w", err)
	}
	return result, next, nil
}

func (s Store) Record(tx *sql.Tx, userID, deviceID, entity, entityID, operation string, version int64, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sync payload for %s/%s: %w", entity, entityID, err)
	}
	_, err = tx.Exec(`INSERT INTO sync_changes (user_id, device_id, entity_type, entity_id, operation, version, payload, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, deviceID, entity, entityID, operation, version, data, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("insert sync change for %s/%s: %w", entity, entityID, err)
	}
	return nil
}
