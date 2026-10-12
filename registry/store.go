package registry

import (
	"context"
	"encoding/json"

	"github.com/euler-gaozl/agent-os-sdk/serviceclient"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	DB         *pgxpool.Pool
	CatalogURL string
	Remote     *serviceclient.Client
}

func (s *Store) PutResource(ctx context.Context, kind, id string, spec json.RawMessage) (Resource, error) {
	if s.CatalogURL != "" && (kind == "skill" || kind == "connector") {
		var result Resource
		e := s.Remote.Request(ctx, "POST", s.CatalogURL+"/v1/assets", map[string]any{"kind": kind, "id": id, "spec": spec}, &result)
		return result, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Resource{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", kind+":"+id); e != nil {
		return Resource{}, e
	}
	var rev int
	e = tx.QueryRow(ctx, "SELECT COALESCE(MAX(revision),0)+1 FROM resources WHERE kind=$1 AND id=$2", kind, id).Scan(&rev)
	if e != nil {
		return Resource{}, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO resources(kind,id,revision,spec) VALUES($1,$2,$3,$4)", kind, id, rev, spec)
	if e != nil {
		return Resource{}, e
	}
	return Resource{Kind: kind, ID: id, Revision: rev, Spec: spec}, tx.Commit(ctx)
}

func (s *Store) Resource(ctx context.Context, kind string, r Ref, dst any) error {
	if s.Remote != nil {
		if s.CatalogURL != "" && (kind == "skill" || kind == "connector") {
			return s.Remote.Resource(ctx, kind, r, dst)
		}
		if s.Remote.RegistryURL != "" {
			return s.Remote.Resource(ctx, kind, r, dst)
		}
	}
	var b []byte
	e := s.DB.QueryRow(ctx, "SELECT spec FROM resources WHERE kind=$1 AND id=$2 AND revision=$3", kind, r.ID, r.Revision).Scan(&b)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, dst)
}

func (s *Store) Resources(ctx context.Context, after ...string) ([]Resource, error) {
	cursor := ""
	if len(after) > 0 {
		cursor = after[0]
	}
	rows, e := s.DB.Query(ctx, `SELECT kind,id,revision,spec FROM resources a WHERE kind!='action'
 AND ($2 OR kind NOT IN ('skill','connector')) AND (kind||':'||id) COLLATE "C">$1
 AND revision=(SELECT max(b.revision) FROM resources b WHERE b.kind=a.kind AND b.id=a.id)
 ORDER BY (kind||':'||id) COLLATE "C" LIMIT 100`, cursor, s.CatalogURL == "")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Resource{}
	size := 0
	for rows.Next() {
		var r Resource
		if e = rows.Scan(&r.Kind, &r.ID, &r.Revision, &r.Spec); e != nil {
			return nil, e
		}
		if s.CatalogURL == "" || (r.Kind != "skill" && r.Kind != "connector") {
			n := len(JSON(r))
			if len(out) > 0 && size+n > 2<<20 {
				break
			}
			size += n
			out = append(out, r)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
