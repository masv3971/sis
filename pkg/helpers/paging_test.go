package helpers

import (
	"math"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

// testMax is the max_limit passed to the parsers under test.
const testMax = 500

func ctxWithQuery(rawQuery string) *gin.Context {
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	gc.Request = httptest.NewRequest("GET", "/?"+rawQuery, nil)
	return gc
}

func TestParseLimitOffset(t *testing.T) {
	cases := []struct {
		query   string
		wantErr bool
		limit   int
		offset  int
	}{
		{"", false, 50, 0},
		{"limit=10&offset=5", false, 10, 5},
		{"limit=" + strconv.Itoa(math.MaxInt), false, testMax, 0},
		{"limit=0", true, 0, 0},
		{"limit=-1", true, 0, 0},
		{"limit=abc", true, 0, 0},
		{"limit=", true, 0, 0},
		{"offset=-1", true, 0, 0},
	}
	for _, c := range cases {
		p, err := ParseLimitOffset(ctxWithQuery(c.query), 50, testMax)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err=%v, wantErr=%v", c.query, err, c.wantErr)
			continue
		}
		if !c.wantErr && (p.Limit != c.limit || p.Offset != c.offset) {
			t.Errorf("%q: got limit=%d offset=%d, want %d %d", c.query, p.Limit, p.Offset, c.limit, c.offset)
		}
	}
}

func TestParsePageNumberSize(t *testing.T) {
	cases := []struct {
		query   string
		wantErr bool
		number  int
		size    int
		offset  int
	}{
		{"", false, 1, 10, 0},
		{"pageNumber=3&pageSize=20", false, 3, 20, 40},
		{"pageSize=" + strconv.Itoa(math.MaxInt), false, 1, testMax, 0},
		{"pageNumber=" + strconv.Itoa(math.MaxInt) + "&pageSize=500", true, 0, 0, 0},
		{"pageSize=0", true, 0, 0, 0},
		{"pageNumber=0", true, 0, 0, 0},
		{"pageNumber=x", true, 0, 0, 0},
	}
	for _, c := range cases {
		n, s, p, err := ParsePageNumberSize(ctxWithQuery(c.query), 10, testMax)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err=%v, wantErr=%v", c.query, err, c.wantErr)
			continue
		}
		if c.wantErr {
			if e := NewErrorFromError(err); e.Status() != 400 {
				t.Errorf("%q: expected 400, got %d", c.query, e.Status())
			}
			continue
		}
		if n != c.number || s != c.size || p.Offset != c.offset || p.Limit != c.size {
			t.Errorf("%q: got n=%d s=%d off=%d lim=%d", c.query, n, s, p.Offset, p.Limit)
		}
	}
}

// TestNonPositiveMaxFailsClosed guards against a hand-built config with
// max_limit 0 turning into an unbounded repository query.
func TestNonPositiveMaxFailsClosed(t *testing.T) {
	if _, err := ParseLimitOffset(ctxWithQuery(""), 50, 0); err == nil {
		t.Fatal("limit/offset: expected error for max 0")
	}
	if _, _, _, err := ParsePageNumberSize(ctxWithQuery(""), 10, 0); err == nil {
		t.Fatal("pageNumber/pageSize: expected error for max 0")
	}
	// A zero default falls back to max rather than to "unbounded".
	if p, err := ParseLimitOffset(ctxWithQuery(""), 0, 7); err != nil || p.Limit != 7 {
		t.Fatalf("zero default: got %d, %v; want 7", p.Limit, err)
	}
}

// TestDefaultsClampedToMax checks that an operator-set max_limit below the
// handler's built-in default still bounds requests that omit limit/pageSize.
func TestDefaultsClampedToMax(t *testing.T) {
	p, err := ParseLimitOffset(ctxWithQuery(""), 100, 20)
	if err != nil || p.Limit != 20 {
		t.Fatalf("limit/offset: got %d, %v; want 20", p.Limit, err)
	}
	_, size, lp, err := ParsePageNumberSize(ctxWithQuery(""), 100, 20)
	if err != nil || size != 20 || lp.Limit != 20 {
		t.Fatalf("pageNumber/pageSize: got %d/%d, %v; want 20", size, lp.Limit, err)
	}
}
