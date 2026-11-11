// Package taskguard shares database fencing without coupling domain packages to jobs.
package taskguard

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var ErrRevoked = errors.New("task ownership revoked")
var ErrConflict = errors.New("active asynchronous task")

type Guard struct {
	JobID, OrgID, DocID, RunVersion, ActorID int64
	Token                                    string
}
type key struct{}

func With(ctx context.Context, g Guard) context.Context { return context.WithValue(ctx, key{}, g) }
func From(ctx context.Context) (Guard, bool)            { g, ok := ctx.Value(key{}).(Guard); return g, ok }
func Token(ctx context.Context) any {
	if g, ok := From(ctx); ok {
		return g.Token
	}
	return nil
}

// Check is called after the parent lock, before any domain publication or reuse.
// The bool selects schema-11 SQL; historical synchronous calls use old statements.
func Check(ctx context.Context, tx pgx.Tx, org, doc int64, claim bool) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('public.knowledge_jobs') IS NOT NULL`).Scan(&exists); err != nil {
		return false, err
	}
	g, async := From(ctx)
	if !exists {
		if async {
			return false, ErrRevoked
		}
		return false, nil
	}
	if !async {
		if claim {
			var busy bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_jobs WHERE org_id=$1 AND document_id=$2 AND status IN ('PENDING','RUNNING','RETRYING'))`, org, doc).Scan(&busy); err != nil {
				return true, err
			}
			if busy {
				return true, ErrConflict
			}
		}
		return true, nil
	}
	if g.OrgID != org || g.DocID != doc || g.Token == "" {
		return true, ErrRevoked
	}
	// The job row lock serializes cancellation/replacement against publication.
	var valid bool
	err := tx.QueryRow(ctx, `SELECT j.status='RUNNING' AND j.run_version=$4 AND j.lease_token=$5 AND j.lease_until>clock_timestamp() AND j.actor_user_id=$6
 AND EXISTS(SELECT 1 FROM organization_members m WHERE m.org_id=j.org_id AND m.user_id=j.actor_user_id AND m.status='active')
 AND EXISTS(SELECT 1 FROM knowledge_documents d WHERE d.org_id=j.org_id AND d.id=j.document_id AND d.deleted_at IS NULL AND d.sha256_hex=j.source_sha256)
 FROM knowledge_jobs j WHERE j.org_id=$1 AND j.document_id=$2 AND j.id=$3 FOR UPDATE`, org, doc, g.JobID, g.RunVersion, g.Token, g.ActorID).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
		return true, ErrRevoked
	}
	return true, err
}
