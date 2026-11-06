//go:build e2e

// helpers_test.go: the fake GitHub for the intake-status suite. Minimal
// issues-list endpoint (request shape copied from the github-intake suite
// so an enabled poller, if it fires mid-assertion, hits the fake and never
// dials live github.com).

package intakestatus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeGitHub struct {
	srv *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octo/forge/issues", func(w http.ResponseWriter, r *http.Request) {
		type ghLabel struct {
			Name string `json:"name"`
		}
		type ghIssue struct {
			Number  int       `json:"number"`
			Title   string    `json:"title"`
			Body    string    `json:"body"`
			Labels  []ghLabel `json:"labels"`
			HTMLURL string    `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		out := []ghIssue{{
			Number:  42,
			Title:   "warp drive ticket",
			Body:    "build the warp drive",
			Labels:  []ghLabel{{Name: "factory"}},
			HTMLURL: "https://github.com/octo/forge/issues/42",
			User: struct {
				Login string `json:"login"`
			}{Login: "octo"},
			UpdatedAt: time.Now().UTC(),
		}}
		payload, _ := json.Marshal(out)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) URL() string { return f.srv.URL }
