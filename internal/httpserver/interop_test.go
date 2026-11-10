package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHubInterop writes a student's records through /api/v1 and reads them
// back through OEAPI and Edu-API: both surfaces must publish the same
// sourcedIds, names and relationships, show an update, and drop a deleted
// person together with their enrollment and result.
func TestHubInterop(t *testing.T) {
	srv := newCorpusServer(t)

	call := func(t *testing.T, token, method, path, body string, want int, out any) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, srv.base+path, rd)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		status, raw := srv.do(t, req)
		require.Equal(t, want, status, "%s %s: %s", method, path, raw)
		if out != nil {
			require.NoError(t, json.Unmarshal(raw, out), "%s %s: decode", method, path)
		}
	}
	// write is setup and runs on the parent test.
	write := func(path, body string) {
		t.Helper()
		call(t, srv.writer, http.MethodPost, "/api/v1/"+path, body, http.StatusCreated, nil)
	}
	read := func(t *testing.T, path string, want int, out any) {
		t.Helper()
		call(t, srv.reader, http.MethodGet, path, "", want, out)
	}

	// Canonical ids differ from sourcedIds: the surfaces must publish the
	// sourcedId, while /api/v1 references use the canonical id.
	write("organizations", `{"id":"o-int","sourcedId":"uni","name":"University"}`)
	write("academic-sessions", `{"id":"s-int","sourcedId":"ht26","title":"HT26"}`)
	write("courses", `{"id":"c-int","sourcedId":"DA101","code":"DA101","title":"Databases","orgId":"o-int"}`)
	write("offerings", `{"id":"f-int","sourcedId":"DA101-ht26","courseId":"c-int","academicSessionId":"s-int","orgId":"o-int"}`)
	write("persons", `{"id":"p-int","sourcedId":"ada","status":"active","givenName":"Ada","familyName":"Lovelace","roles":["student"]}`)
	write("persons", `{"id":"p-grace","sourcedId":"grace","status":"active","givenName":"Grace","familyName":"Hopper","roles":["faculty"]}`)
	write("enrollments", `{"id":"e-int","sourcedId":"enr-ada","personId":"p-int","offeringId":"f-int","role":"student"}`)
	write("enrollments", `{"id":"e-grace","sourcedId":"enr-grace","personId":"p-grace","offeringId":"f-int","role":"faculty"}`)
	write("results", `{"id":"r-int","sourcedId":"res-ada","personId":"p-int","courseId":"c-int","offeringId":"f-int","grade":"A","credits":7.5}`)

	type text []struct {
		Value string `json:"value"`
	}
	type oePerson struct {
		PersonID  string `json:"personId"`
		GivenName string `json:"givenName"`
		Surname   string `json:"surname"`
	}
	type eduPerson struct {
		SourcedID string `json:"sourcedId"`
		LegalName struct {
			GivenName  string `json:"givenName"`
			FamilyName string `json:"familyName"`
		} `json:"legalName"`
	}

	t.Run("organization", func(t *testing.T) {
		var oe struct{ OrganizationID, Name string }
		read(t, "/oeapi/organizations/uni", http.StatusOK, &oe)
		var edu struct {
			SourcedID string `json:"sourcedId"`
			Name      text   `json:"name"`
		}
		read(t, eduBase+"/organizations/uni", http.StatusOK, &edu)
		assert.Equal(t, "uni", oe.OrganizationID)
		assert.Equal(t, "uni", edu.SourcedID)
		assert.Equal(t, "University", oe.Name)
		require.Len(t, edu.Name, 1)
		assert.Equal(t, "University", edu.Name[0].Value)
	})

	t.Run("academic session", func(t *testing.T) {
		var oe struct{ AcademicSessionID, Name string }
		read(t, "/oeapi/academic-sessions/ht26", http.StatusOK, &oe)
		var edu struct {
			SourcedID string `json:"sourcedId"`
			Title     text   `json:"title"`
		}
		read(t, eduBase+"/academicSessions/ht26", http.StatusOK, &edu)
		assert.Equal(t, "ht26", oe.AcademicSessionID)
		assert.Equal(t, "ht26", edu.SourcedID)
		assert.Equal(t, "HT26", oe.Name)
		require.Len(t, edu.Title, 1)
		assert.Equal(t, "HT26", edu.Title[0].Value)
	})

	t.Run("course", func(t *testing.T) {
		var oe struct{ CourseID, Name, OrganizationID string }
		read(t, "/oeapi/courses/DA101", http.StatusOK, &oe)
		var edu struct {
			SourcedID    string `json:"sourcedId"`
			Title        text   `json:"title"`
			Organization string `json:"organization"`
		}
		read(t, eduBase+"/courseTemplates/DA101", http.StatusOK, &edu)
		assert.Equal(t, "DA101", oe.CourseID)
		assert.Equal(t, "DA101", edu.SourcedID)
		assert.Equal(t, "Databases", oe.Name)
		require.Len(t, edu.Title, 1)
		assert.Equal(t, "Databases", edu.Title[0].Value)
		assert.Equal(t, "uni", oe.OrganizationID)
		assert.Equal(t, "uni", edu.Organization)
	})

	t.Run("offering", func(t *testing.T) {
		var oe struct{ OfferingID, CourseID, AcademicSessionID string }
		read(t, "/oeapi/offerings/DA101-ht26", http.StatusOK, &oe)
		var edu struct {
			SourcedID       string `json:"sourcedId"`
			Course          string `json:"course"`
			AcademicSession string `json:"academicSession"`
			Organization    string `json:"organization"`
		}
		read(t, eduBase+"/courseOfferings/DA101-ht26", http.StatusOK, &edu)
		assert.Equal(t, "DA101-ht26", oe.OfferingID)
		assert.Equal(t, "DA101-ht26", edu.SourcedID)
		assert.Equal(t, "DA101", oe.CourseID)
		assert.Equal(t, "DA101", edu.Course)
		assert.Equal(t, "ht26", oe.AcademicSessionID)
		assert.Equal(t, "ht26", edu.AcademicSession)
		assert.Equal(t, "uni", edu.Organization)
	})

	t.Run("person and enrollment", func(t *testing.T) {
		var oe oePerson
		read(t, "/oeapi/persons/ada", http.StatusOK, &oe)
		var edu eduPerson
		read(t, eduBase+"/persons/ada", http.StatusOK, &edu)
		assert.Equal(t, "ada", oe.PersonID)
		assert.Equal(t, "ada", edu.SourcedID)
		assert.Equal(t, "Ada", oe.GivenName)
		assert.Equal(t, "Ada", edu.LegalName.GivenName)
		assert.Equal(t, "Lovelace", oe.Surname)
		assert.Equal(t, "Lovelace", edu.LegalName.FamilyName)

		var oeEnr struct {
			AssociationID, PersonID, OfferingID, Role string
		}
		read(t, "/oeapi/associations/enr-ada", http.StatusOK, &oeEnr)
		var eduEnr struct {
			SourcedID         string `json:"sourcedId"`
			Person            string `json:"person"`
			EducationOffering string `json:"educationOffering"`
			Role              string `json:"role"`
		}
		read(t, eduBase+"/enrollments/enr-ada", http.StatusOK, &eduEnr)
		assert.Equal(t, "enr-ada", oeEnr.AssociationID)
		assert.Equal(t, "enr-ada", eduEnr.SourcedID)
		assert.Equal(t, "ada", oeEnr.PersonID)
		assert.Equal(t, "ada", eduEnr.Person)
		assert.Equal(t, "DA101-ht26", oeEnr.OfferingID)
		assert.Equal(t, "DA101-ht26", eduEnr.EducationOffering)
		assert.Equal(t, "student", oeEnr.Role)
		assert.Equal(t, "student", eduEnr.Role)

		// OEAPI publishes the stored role, Edu-API maps faculty to its term.
		read(t, "/oeapi/associations/enr-grace", http.StatusOK, &oeEnr)
		read(t, eduBase+"/enrollments/enr-grace", http.StatusOK, &eduEnr)
		assert.Equal(t, "faculty", oeEnr.Role)
		assert.Equal(t, "teacher", eduEnr.Role)
	})

	t.Run("result is OEAPI only", func(t *testing.T) {
		var oe struct {
			ResultID, PersonID, CourseID, OfferingID, Score string
			ECTS                                            float64
		}
		read(t, "/oeapi/results/res-ada", http.StatusOK, &oe)
		assert.Equal(t, "res-ada", oe.ResultID)
		assert.Equal(t, "ada", oe.PersonID)
		assert.Equal(t, "DA101", oe.CourseID)
		assert.Equal(t, "DA101-ht26", oe.OfferingID)
		assert.Equal(t, "A", oe.Score)
		assert.InDelta(t, 7.5, oe.ECTS, 0)

		var nested struct {
			Result *struct{ ResultID string }
		}
		read(t, "/oeapi/associations/enr-ada", http.StatusOK, &nested)
		require.NotNil(t, nested.Result, "association must nest the result")
		assert.Equal(t, "res-ada", nested.Result.ResultID)
	})

	t.Run("lists agree", func(t *testing.T) {
		var oe struct {
			Items []oePerson
		}
		read(t, "/oeapi/persons", http.StatusOK, &oe)
		var edu []eduPerson
		read(t, eduBase+"/persons", http.StatusOK, &edu)
		require.Len(t, oe.Items, 2)
		require.Len(t, edu, 2)
		assert.ElementsMatch(t, []string{oe.Items[0].PersonID, oe.Items[1].PersonID}, []string{edu[0].SourcedID, edu[1].SourcedID})

		var oeEnr struct {
			Items []struct{ AssociationID string }
		}
		read(t, "/oeapi/associations", http.StatusOK, &oeEnr)
		var eduEnr []struct {
			SourcedID string `json:"sourcedId"`
		}
		read(t, eduBase+"/enrollments", http.StatusOK, &eduEnr)
		require.Len(t, oeEnr.Items, 2)
		require.Len(t, eduEnr, 2)
		assert.ElementsMatch(t, []string{oeEnr.Items[0].AssociationID, oeEnr.Items[1].AssociationID}, []string{eduEnr[0].SourcedID, eduEnr[1].SourcedID})
	})

	eduIDs := func(t *testing.T, path string) []string {
		t.Helper()
		var items []struct {
			SourcedID string `json:"sourcedId"`
		}
		read(t, eduBase+path, http.StatusOK, &items)
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.SourcedID)
		}
		return ids
	}

	t.Run("edu-api nested routes", func(t *testing.T) {
		assert.Equal(t, []string{"ada"}, eduIDs(t, "/students"))
		assert.Equal(t, []string{"grace"}, eduIDs(t, "/staff"))
		offering := "/courseOfferings/DA101-ht26"
		assert.ElementsMatch(t, []string{"enr-ada", "enr-grace"}, eduIDs(t, offering+"/enrollments"))
		assert.Equal(t, []string{"ada"}, eduIDs(t, offering+"/students"))
		assert.Equal(t, []string{"grace"}, eduIDs(t, offering+"/staff"))
		assert.Equal(t, []string{"DA101-ht26"}, eduIDs(t, "/academicSessions/ht26/courseOfferings"))
		assert.ElementsMatch(t, []string{"enr-ada", "enr-grace"}, eduIDs(t, "/academicSessions/ht26/enrollments"))
		read(t, eduBase+"/courseOfferings/none/enrollments", http.StatusNotFound, nil)
	})

	t.Run("edu-api sort and filter", func(t *testing.T) {
		assert.Equal(t, []string{"ada", "grace"}, eduIDs(t, "/persons?sort=legalName.givenName"))
		assert.Equal(t, []string{"grace", "ada"}, eduIDs(t, "/persons?sort=legalName.givenName&orderBy=desc"))
		assert.Equal(t, []string{"grace"}, eduIDs(t, "/persons?filter="+url.QueryEscape("legalName.familyName='Hopper'")))
	})

	t.Run("update shows on both", func(t *testing.T) {
		call(t, srv.writer, http.MethodPut, "/api/v1/persons/p-int",
			`{"sourcedId":"ada","status":"active","givenName":"Augusta","familyName":"Lovelace","roles":["student"]}`, http.StatusOK, nil)
		var oe oePerson
		read(t, "/oeapi/persons/ada", http.StatusOK, &oe)
		var edu eduPerson
		read(t, eduBase+"/persons/ada", http.StatusOK, &edu)
		assert.Equal(t, "Augusta", oe.GivenName)
		assert.Equal(t, "Augusta", edu.LegalName.GivenName)
	})

	// Runs last: it deletes records.
	t.Run("delete cascades to both", func(t *testing.T) {
		call(t, srv.writer, http.MethodDelete, "/api/v1/persons/p-int", "", http.StatusNoContent, nil)
		for _, path := range []string{
			"/oeapi/persons/ada",
			"/oeapi/associations/enr-ada",
			"/oeapi/results/res-ada",
			eduBase + "/persons/ada",
			eduBase + "/enrollments/enr-ada",
		} {
			read(t, path, http.StatusNotFound, nil)
		}
		// The catalog and the other person stay.
		read(t, "/oeapi/offerings/DA101-ht26", http.StatusOK, nil)
		read(t, eduBase+"/courseOfferings/DA101-ht26", http.StatusOK, nil)
		assert.Empty(t, eduIDs(t, "/students"))
		assert.Equal(t, []string{"grace"}, eduIDs(t, "/staff"))
		assert.Equal(t, []string{"enr-grace"}, eduIDs(t, "/courseOfferings/DA101-ht26/enrollments"))
	})
}
