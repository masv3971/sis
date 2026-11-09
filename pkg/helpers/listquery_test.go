package helpers

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/port"
)

var testFields = ListFields{
	"sourcedId":           "sourcedId",
	"legalName.givenName": "givenName",
	"title":               "title",
}

func parse(t *testing.T, query string) (port.ListParams, error) {
	t.Helper()
	return ParseListQuery(ctxWithQuery(query), 50, testMax, testFields)
}

func filterQuery(f string) string { return "filter=" + url.QueryEscape(f) }

func TestParseListQuerySort(t *testing.T) {
	params, err := parse(t, "sort=legalName.givenName")
	require.NoError(t, err)
	assert.Equal(t, "givenName", params.Sort, "the stored key, not the published name")
	assert.False(t, params.Desc)

	for query, desc := range map[string]bool{"orderBy=asc": false, "orderBy=DESC": true} {
		params, err = parse(t, "sort=title&"+query)
		require.NoError(t, err, query)
		assert.Equal(t, desc, params.Desc, query)
	}

	for _, bad := range []string{"sort=nope", "sort=givenName", "orderBy=asc", "sort=title&orderBy=up", "sort=title&orderBy="} {
		_, err = parse(t, bad)
		require.Error(t, err, bad)
		assert.Equal(t, 400, NewErrorFromError(err).Status(), bad)
	}
}

func TestParseListQueryFilter(t *testing.T) {
	tests := []struct {
		filter string
		want   port.Filter
	}{
		{"title='A'", port.Filter{Terms: []port.FilterTerm{{Field: "title", Op: port.FilterEquals, Value: "A"}}}},
		{"title!='A'", port.Filter{Terms: []port.FilterTerm{{Field: "title", Op: port.FilterNotEquals, Value: "A"}}}},
		{"title~'A b'", port.Filter{Terms: []port.FilterTerm{{Field: "title", Op: port.FilterContains, Value: "A b"}}}},
		{"legalName.givenName='Jo'", port.Filter{Terms: []port.FilterTerm{{Field: "givenName", Op: port.FilterEquals, Value: "Jo"}}}},
		{"title='O''Brien'", port.Filter{Terms: []port.FilterTerm{{Field: "title", Op: port.FilterEquals, Value: "O'Brien"}}}},
		{"title=''", port.Filter{Terms: []port.FilterTerm{{Field: "title", Op: port.FilterEquals, Value: ""}}}},
		{"title='x AND y'", port.Filter{Terms: []port.FilterTerm{{Field: "title", Op: port.FilterEquals, Value: "x AND y"}}}},
		{"title='a' AND sourcedId='b'", port.Filter{Terms: []port.FilterTerm{
			{Field: "title", Op: port.FilterEquals, Value: "a"}, {Field: "sourcedId", Op: port.FilterEquals, Value: "b"}}}},
		{"title='a' OR title~'b'", port.Filter{Any: true, Terms: []port.FilterTerm{
			{Field: "title", Op: port.FilterEquals, Value: "a"}, {Field: "title", Op: port.FilterContains, Value: "b"}}}},
		{"title='a' and title='b'", port.Filter{Terms: []port.FilterTerm{
			{Field: "title", Op: port.FilterEquals, Value: "a"}, {Field: "title", Op: port.FilterEquals, Value: "b"}}}},
	}
	for _, tt := range tests {
		params, err := parse(t, filterQuery(tt.filter))
		require.NoError(t, err, tt.filter)
		require.NotNil(t, params.Filter, tt.filter)
		assert.Equal(t, tt.want, *params.Filter, tt.filter)
	}
}

func TestParseListQueryFilterErrors(t *testing.T) {
	tests := []struct {
		filter string
		title  string
	}{
		{"nope='a'", ErrInvalidFilterField},
		{"givenName='a'", ErrInvalidFilterField}, // the stored key is not a published name
		{"title='a' AND nope='b'", ErrInvalidFilterField},
		{"title>'a'", ErrValidation},
		{"title>='a'", ErrValidation},
		{"title<'a'", ErrValidation},
		{"title<='a'", ErrValidation},
		{"title=a", ErrValidation},
		{"title='a", ErrValidation},
		{"title", ErrValidation},
		{"='a'", ErrValidation},
		{"title='a' title='b'", ErrValidation},
		{"title='a'AND title='b'", ErrValidation},
		{"title='a' AND title='b' AND title='c'", ErrValidation},
		{"title='a' AND title='b' OR title='c'", ErrValidation},
		{"title='a' AND", ErrValidation},
		{"title='a' AND ", ErrValidation},
	}
	for _, tt := range tests {
		_, err := parse(t, filterQuery(tt.filter))
		require.Error(t, err, tt.filter)
		apiErr := NewErrorFromError(err)
		assert.Equal(t, tt.title, apiErr.Title, tt.filter)
		assert.Equal(t, 400, apiErr.Status(), tt.filter)
	}
}

func TestParseListQueryFilterValueIsBounded(t *testing.T) {
	long := make([]byte, maxFilterValue+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err := parse(t, filterQuery("title='"+string(long)+"'"))
	require.Error(t, err)
	_, err = parse(t, filterQuery("title='"+string(long[:maxFilterValue])+"'"))
	assert.NoError(t, err, "the limit itself is allowed")
}

func TestParseListQueryKeepsLimitOffset(t *testing.T) {
	params, err := parse(t, "limit=5&offset=2&sort=title&orderBy=desc&"+filterQuery("title~'x'"))
	require.NoError(t, err)
	assert.Equal(t, 5, params.Limit)
	assert.Equal(t, 2, params.Offset)
	assert.Equal(t, "title", params.Sort)
	assert.True(t, params.Desc)
	require.NotNil(t, params.Filter)

	_, err = parse(t, "limit=0")
	assert.Error(t, err)
}

func TestParseListQueryNilFieldsRejects(t *testing.T) {
	for _, query := range []string{"sort=title", "filter=" + url.QueryEscape("title='a'")} {
		_, err := ParseListQuery(ctxWithQuery(query), 50, testMax, nil)
		require.Error(t, err, query)
		assert.Equal(t, 400, NewErrorFromError(err).Status(), query)
	}
	params, err := ParseListQuery(ctxWithQuery("orderBy=desc"), 50, testMax, nil)
	require.NoError(t, err, "orderBy alone is ignored like any unknown parameter")
	assert.Empty(t, params.Sort)
}
