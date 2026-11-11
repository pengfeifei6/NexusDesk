package integration

import (
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
	"os"
	"reflect"
	"testing"
)

// Same-timestamp rows exercise the ID tie-break, every filter shape and page.
func TestWeek09ListQueryEquivalence(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W09_CACHE") != "1" {
		t.Skip("requires owned W09 harness")
	}
	f := week06Fixture(t)
	f.exec(t, `INSERT INTO tickets(org_id,creator_id,title,body,priority,status,assignee_id,created_at,deleted_at)
 SELECT $1,$2,'Synthetic query','Fixture',1+n%3,CASE WHEN n%2=0 THEN 'OPEN' ELSE 'CLOSED' END,$2,'2026-01-01',CASE WHEN n%7=0 THEN now() ELSE NULL END FROM generate_series(1,55)n`, f.org, f.uploader)
	tr := ticket.NewPostgresRepository(f.pool)
	for _, status := range []ticket.Status{"", ticket.Open, ticket.Closed} {
		for _, priority := range []int16{0, 1, 2, 3} {
			for page := int64(1); page <= 6; page++ {
				filter := ticket.Filter{Page: ticket.Page{Number: page, Size: 5}, Status: status}
				var sp any
				var pp any
				if status != "" {
					sp = string(status)
				}
				if priority != 0 {
					p := priority
					filter.Priority = &p
					pp = priority
				}
				got, e := tr.ListTickets(f.ctx, f.org, filter)
				if e != nil {
					t.Fatal(e)
				}
				rows, e := f.pool.Query(f.ctx, `SELECT id FROM tickets WHERE org_id=$1 AND deleted_at IS NULL AND ($2::text IS NULL OR status=$2) AND ($3::smallint IS NULL OR priority=$3) ORDER BY created_at DESC,id DESC LIMIT 5 OFFSET $4`, f.org, sp, pp, (page-1)*5)
				if e != nil {
					t.Fatal(e)
				}
				want := []int64{}
				for rows.Next() {
					var id int64
					if e = rows.Scan(&id); e != nil {
						t.Fatal(e)
					}
					want = append(want, id)
				}
				rows.Close()
				if rows.Err() != nil {
					t.Fatal(rows.Err())
				}
				ids := []int64{}
				for _, v := range got {
					ids = append(ids, v.ID)
				}
				if !reflect.DeepEqual(ids, want) {
					t.Fatalf("ticket filter/page mismatch: %v %v", ids, want)
				}
			}
		}
	}
	f.exec(t, `INSERT INTO knowledge_documents(org_id,uploaded_by,original_filename,storage_key,media_type,size_bytes,sha256_hex,status,extracted_chars,created_at,deleted_at)
 SELECT $1,$2,'query.txt',md5(($1::bigint)::text||':'||n::text)||md5(n::text),'text/plain',12,repeat('a',64),CASE WHEN n%2=0 THEN 'READY' ELSE 'UPLOADED' END,CASE WHEN n%2=0 THEN 12 ELSE 0 END,'2026-01-01',CASE WHEN n%7=0 THEN now() ELSE NULL END FROM generate_series(1,55)n`, f.org, f.uploader)
	dr := knowledge.NewPostgresRepository(f.pool)
	for _, status := range []knowledge.Status{"", knowledge.Ready, knowledge.Uploaded, knowledge.Failed} {
		for page := int64(1); page <= 12; page++ {
			got, e := dr.List(f.ctx, f.org, knowledge.Filter{Page: page, PageSize: 5, Status: status})
			if e != nil {
				t.Fatal(e)
			}
			rows, e := f.pool.Query(f.ctx, `SELECT id FROM knowledge_documents WHERE org_id=$1 AND deleted_at IS NULL AND ($2='' OR status=$2) ORDER BY created_at DESC,id DESC LIMIT 5 OFFSET $3`, f.org, string(status), (page-1)*5)
			if e != nil {
				t.Fatal(e)
			}
			want := []int64{}
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					t.Fatal(e)
				}
				want = append(want, id)
			}
			rows.Close()
			if rows.Err() != nil {
				t.Fatal(rows.Err())
			}
			ids := []int64{}
			for _, v := range got {
				ids = append(ids, v.ID)
			}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("document filter/page mismatch: %v %v", ids, want)
			}
		}
	}
}
