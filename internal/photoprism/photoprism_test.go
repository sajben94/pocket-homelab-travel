package photoprism

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// PhotoPrism with merged=true limits files, not photos: every photo here
// has two files (RAW + JPEG), so a batch of 500 rows holds 250 photos.
func TestSearchPagesPastMergedBatches(t *testing.T) {
	type row struct {
		UID, Type, TakenAtLocal, Hash, FileName string
	}
	var rows []row
	for d := 19; d <= 26; d++ {
		for i := 0; i < 120; i++ {
			uid := fmt.Sprintf("pq%02d%03d", d, i)
			at := fmt.Sprintf("2025-07-%02dT%02d:%02d:00Z", d, 8+i/10, i%60)
			rows = append(rows, row{uid, "image", at, uid + "raw", "IMG.dng"}, row{uid, "image", at, uid + "jpg", "IMG.jpg"})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		off, _ := strconv.Atoi(q.Get("offset"))
		cnt, _ := strconv.Atoi(q.Get("count"))
		var sel []row
		for _, x := range rows {
			if x.TakenAtLocal[:10] >= q.Get("after") && x.TakenAtLocal[:10] < q.Get("before") {
				sel = append(sel, x)
			}
		}
		if off > len(sel) {
			off = len(sel)
		}
		end := min(off+cnt, len(sel))
		var out []row // merged: consecutive files of one photo become one
		for _, x := range sel[off:end] {
			if len(out) > 0 && out[len(out)-1].UID == x.UID {
				continue
			}
			out = append(out, x)
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	ps, err := New(srv.URL, "", 5*time.Second).Search(context.Background(), "2025-07-19", "2025-07-26", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	days := map[string]int{}
	for _, p := range ps {
		days[p.TakenAt[:10]]++
	}
	if len(ps) != 8*120 || len(days) != 8 {
		t.Fatalf("want 960 photos on 8 days, got %d on %d days: %v", len(ps), len(days), days)
	}
}
