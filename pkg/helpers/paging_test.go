package helpers

import (
	"math"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"limit=", true, 0, 0}, // empty value is present (gin's GetQuery), not absent
		{"offset=", true, 0, 0},
		{"offset=-1", true, 0, 0},
		// sort/filter are not implemented, so they must not be silently ignored.
		{"sort=familyName", true, 0, 0},
		{"filter=status%3D%27active%27", true, 0, 0},
		{"sort=&filter=", false, 50, 0}, // empty means "not requested"
	}
	for _, tc := range cases {
		params, err := ParseLimitOffset(ctxWithQuery(tc.query), 50, testMax)
		if tc.wantErr {
			if assert.Error(t, err, "%q", tc.query) {
				assert.Equal(t, 400, NewErrorFromError(err).Status(), "%q: status", tc.query)
			}
			continue
		}
		if !assert.NoError(t, err, "%q", tc.query) {
			continue
		}
		assert.Equal(t, tc.limit, params.Limit, "%q: limit", tc.query)
		assert.Equal(t, tc.offset, params.Offset, "%q: offset", tc.query)
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
		{"pageNumber=", true, 0, 0, 0}, // empty value is present, not absent
		{"pageSize=", true, 0, 0, 0},
	}
	for _, tc := range cases {
		number, size, params, err := ParsePageNumberSize(ctxWithQuery(tc.query), 10, testMax)
		if tc.wantErr {
			if assert.Error(t, err, "%q", tc.query) {
				assert.Equal(t, 400, NewErrorFromError(err).Status(), "%q: status", tc.query)
			}
			continue
		}
		if !assert.NoError(t, err, "%q", tc.query) {
			continue
		}
		assert.Equal(t, tc.number, number, "%q: pageNumber", tc.query)
		assert.Equal(t, tc.size, size, "%q: pageSize", tc.query)
		assert.Equal(t, tc.offset, params.Offset, "%q: offset", tc.query)
		assert.Equal(t, tc.size, params.Limit, "%q: limit", tc.query)
	}
}

// TestNonPositiveMaxFailsClosed guards against a hand-built config with
// max_limit 0 turning into an unbounded repository query.
func TestNonPositiveMaxFailsClosed(t *testing.T) {
	_, err := ParseLimitOffset(ctxWithQuery(""), 50, 0)
	assert.Error(t, err, "limit/offset: expected error for max 0")
	_, _, _, err = ParsePageNumberSize(ctxWithQuery(""), 10, 0)
	assert.Error(t, err, "pageNumber/pageSize: expected error for max 0")
	// A zero default falls back to max rather than to "unbounded".
	params, err := ParseLimitOffset(ctxWithQuery(""), 0, 7)
	require.NoError(t, err, "zero default")
	assert.Equal(t, 7, params.Limit, "zero default")
}

// TestDefaultsClampedToMax checks that an operator-set max_limit below the
// handler's built-in default still bounds requests that omit limit/pageSize.
func TestDefaultsClampedToMax(t *testing.T) {
	params, err := ParseLimitOffset(ctxWithQuery(""), 100, 20)
	require.NoError(t, err, "limit/offset")
	assert.Equal(t, 20, params.Limit, "limit/offset")
	_, size, pageParams, err := ParsePageNumberSize(ctxWithQuery(""), 100, 20)
	require.NoError(t, err, "pageNumber/pageSize")
	assert.Equal(t, 20, size, "pageNumber/pageSize: size")
	assert.Equal(t, 20, pageParams.Limit, "pageNumber/pageSize: limit")
}
