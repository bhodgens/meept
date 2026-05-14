package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// OpenAIAdapter manages fine-tuning via OpenAI API.
type OpenAIAdapter struct {
	apiKey  string
	baseURL string
	client  *http.Client
	orgID   string
}

// NewOpenAIAdapter creates a new OpenAI adapter manager.
func NewOpenAIAdapter(apiKey, orgID string) *OpenAIAdapter {
	return &OpenAIAdapter{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		orgID: orgID,
	}
}

// FineTuneJob represents an OpenAI fine-tuning job.
type FineTuneJob struct {
	ID             string    `json:"id"`
	Object         string    `json:"object"`
	Model          string    `json:"model"`
	CreatedAt      int64     `json:"created_at"`
	FinishedAt     *int64    `json:"finished_at,omitempty"`
	FineTunedModel string    `json:"fine_tuned_model,omitempty"`
	Status         string    `json:"status"`
	TrainingFile   string    `json:"training_file"`
	ValidationFile string    `json:"validation_file,omitempty"`
	Error          *JobError `json:"error,omitempty"`
}

// JobError represents an error in a fine-tuning job.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// UploadedFile represents an uploaded training file.
type UploadedFile struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
}

// UploadTrainingFile uploads a JSONL training file to OpenAI.
func (a *OpenAIAdapter) UploadTrainingFile(ctx context.Context, filePath string) (*UploadedFile, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Create multipart request
	body := &bytes.Buffer{}
	body.WriteString("--boundary\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"purpose\"\r\n\r\n")
	body.WriteString("fine-tune\r\n")
	body.WriteString("--boundary\r\n")
	fmt.Fprintf(body, "Content-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\n", filePath)
	body.WriteString("Content-Type: application/json\r\n\r\n")
	body.Write(content)
	body.WriteString("\r\n--boundary--\r\n")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/files", body)
	if err != nil {
		return nil, err
	}

	a.setHeaders(req)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result UploadedFile
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// CreateFineTuneJob creates a new fine-tuning job.
func (a *OpenAIAdapter) CreateFineTuneJob(ctx context.Context, trainingFileID, model string, hyperparams map[string]any) (*FineTuneJob, error) {
	payload := map[string]any{
		"training_file": trainingFileID,
		"model":         model,
	}

	// Add hyperparameters
	if len(hyperparams) > 0 {
		payload["hyperparameters"] = hyperparams
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/fine_tuning/jobs", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create job failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create job failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result FineTuneJob
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetFineTuneJob retrieves the status of a fine-tuning job.
func (a *OpenAIAdapter) GetFineTuneJob(ctx context.Context, jobID string) (*FineTuneJob, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/fine_tuning/jobs/"+jobID, http.NoBody)
	if err != nil {
		return nil, err
	}

	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get job failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get job failed: %d", resp.StatusCode)
	}

	var result FineTuneJob
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListFineTuneJobs lists all fine-tuning jobs.
func (a *OpenAIAdapter) ListFineTuneJobs(ctx context.Context, limit int) ([]*FineTuneJob, error) {
	url := fmt.Sprintf("%s/fine_tuning/jobs?limit=%d", a.baseURL, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list jobs failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list jobs failed: %d", resp.StatusCode)
	}

	var result struct {
		Data []*FineTuneJob `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// CancelFineTuneJob cancels a running fine-tuning job.
func (a *OpenAIAdapter) CancelFineTuneJob(ctx context.Context, jobID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/fine_tuning/jobs/"+jobID+"/cancel", http.NoBody)
	if err != nil {
		return err
	}

	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("cancel job failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cancel job failed: %d", resp.StatusCode)
	}

	return nil
}

// WaitForJob polls the job status until completion.
func (a *OpenAIAdapter) WaitForJob(ctx context.Context, jobID string, pollInterval time.Duration) (*FineTuneJob, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			job, err := a.GetFineTuneJob(ctx, jobID)
			if err != nil {
				return nil, err
			}

			switch job.Status {
			case "succeeded":
				return job, nil
			case "failed", "cancelled":
				if job.Error != nil {
					return job, fmt.Errorf("job %s: %s - %s", job.Status, job.Error.Code, job.Error.Message)
				}
				return job, fmt.Errorf("job %s", job.Status)
			}
			// Continue polling for "validating_files", "queued", "running"
		}
	}
}

// DeleteFile deletes an uploaded file.
func (a *OpenAIAdapter) DeleteFile(ctx context.Context, fileID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.baseURL+"/files/"+fileID, http.NoBody)
	if err != nil {
		return err
	}

	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete file failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete file failed: %d", resp.StatusCode)
	}

	return nil
}

// DeleteModel deletes a fine-tuned model.
func (a *OpenAIAdapter) DeleteModel(ctx context.Context, modelID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.baseURL+"/models/"+modelID, http.NoBody)
	if err != nil {
		return err
	}

	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete model failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete model failed: %d", resp.StatusCode)
	}

	return nil
}

func (a *OpenAIAdapter) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if a.orgID != "" {
		req.Header.Set("OpenAI-Organization", a.orgID)
	}
}
