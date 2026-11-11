package observe

import (
	"context"
	"github.com/jackc/pgx/v5"
	"runtime/trace"
	"strings"
	"time"
)

type QueryTracer struct{ registry *Registry }
type queryKey struct{}
type querySpan struct {
	started   time.Time
	operation string
	region    *trace.Region
}

func NewQueryTracer(r *Registry) *QueryTracer { return &QueryTracer{registry: r} }
func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	// Inspect only the first token; never keep SQL, argument values or error text.
	sql := strings.TrimLeft(data.SQL, " \n\r\t")
	end := strings.IndexAny(sql, " \n\r\t;")
	if end >= 0 {
		sql = sql[:end]
	}
	if len(sql) > 8 {
		sql = "other"
	}
	op := sqlOperation(strings.ToLower(sql))
	return context.WithValue(ctx, queryKey{}, querySpan{time.Now(), op, trace.StartRegion(ctx, "db."+op)})
}
func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if span, ok := ctx.Value(queryKey{}).(querySpan); ok {
		span.region.End()
		t.registry.RecordSQL(span.operation, data.Err, time.Since(span.started))
	}
}
