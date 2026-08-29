package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

type fakeModelClient struct {
	recs      []llm.ModelRecord
	pullCalls int
	pullErr   error
}

func (f *fakeModelClient) List() []llm.ModelRecord { return f.recs }

func (f *fakeModelClient) Get(name string) (llm.ModelRecord, bool) {
	for _, r := range f.recs {
		if r.Name == name {
			return r, true
		}
	}
	return llm.ModelRecord{}, false
}

func (f *fakeModelClient) Pull(ctx context.Context, repoID, quant string, progress func(done, total int64)) (*llm.ModelRecord, error) {
	f.pullCalls++
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	if progress != nil {
		progress(0, 100)
		progress(50, 100)
		progress(100, 100)
	}
	rec := &llm.ModelRecord{Name: repoID + "-m", RepoID: repoID, File: "/tmp/m.gguf", Bytes: 100, SHA256: "abc"}
	f.recs = append(f.recs, *rec)
	return rec, nil
}

func TestRunModelPull_ProgressLowercaseHumanized(t *testing.T) {
	var out bytes.Buffer
	client := &fakeModelClient{}
	if err := runModelPull(context.Background(), client, "org/repo", "q4_k_m", false, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "Downloading") || strings.Contains(s, "Pulled") {
		t.Errorf("output not lowercase: %q", s)
	}
	if !strings.Contains(s, "downloading") || !strings.Contains(s, "100 b") {
		t.Errorf("expected humanized progress lines, got: %q", s)
	}
	if client.pullCalls != 1 {
		t.Errorf("pull calls = %d, want 1", client.pullCalls)
	}
}

func TestRunModelList_TextAndJSON(t *testing.T) {
	client := &fakeModelClient{recs: []llm.ModelRecord{{
		Name: "org-repo-m-q4_k_m", RepoID: "org/repo", File: "/tmp/m.gguf",
		Bytes: 2048, SHA256: "deadbeef", AddedAt: time.Unix(0, 0).UTC(),
	}}}

	var out bytes.Buffer
	if err := runModelList(client, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "org-repo-m-q4_k_m") || !strings.Contains(out.String(), "2.0 Kb") {
		t.Errorf("text list wrong: %q", out.String())
	}

	out.Reset()
	if err := runModelList(client, true, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"repo_id": "org/repo"`) {
		t.Errorf("json list wrong: %q", out.String())
	}
}

func TestRunModelTest_NotFoundAndProbe(t *testing.T) {
	client := &fakeModelClient{recs: []llm.ModelRecord{{Name: "org-repo-m"}}}
	err := runModelTest(context.Background(), client, "missing", nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}

	var out bytes.Buffer
	called := 0
	probe := func(ctx context.Context, baseURL, model string) (time.Duration, error) {
		called++
		if model != "org-repo-m" {
			t.Errorf("probe model = %q", model)
		}
		return 42 * time.Millisecond, nil
	}
	if err := runModelTest(context.Background(), client, "org-repo-m", probe, &out); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("probe calls = %d", called)
	}
	if !strings.Contains(out.String(), "42ms") {
		t.Errorf("latency not reported: %q", out.String())
	}
}
