package registry

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

const readinessQuery = "SELECT r.kind,r.revision,r.spec,s.sealed FROM resources r,secrets s WHERE false"

// Readiness checks only this service's schema and permissions without reading user data.
func Readiness(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return errors.New("database required")
	}
	_, err := db.Exec(ctx, readinessQuery)
	return err
}
