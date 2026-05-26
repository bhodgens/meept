package shadow

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Preference indicates which response was preferred.
type Preference string

const (
	PreferenceStudent Preference = "student"
	PreferenceTeacher Preference = "teacher"
	PreferenceTie     Preference = "tie"
)

// Domain categorizes the type of content.
type Domain string

const (
	DomainCode      Domain = "code"
	DomainGeneral   Domain = "general"
	DomainPlanning  Domain = "planning"
	DomainDebugging Domain = "debugging"
	DomainAnalysis  Domain = "analysis"
)

// TaskType categorizes the type of task.
type TaskType string

const (
	TaskTypeChat      TaskType = "chat"
	TaskTypeToolUse   TaskType = "tool_use"
	TaskTypeReasoning TaskType = "reasoning"
	TaskTypeMultiStep TaskType = "multi_step"
)

// Message role constants.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Classification keyword constants used across classifier, manager, and middleware.
const (
	KwFix      = "fix"
	KwFunction = "function"
	KwAnalyze  = "analyze"
	KwEvaluate = "evaluate"
	KwFirst    = "first"
	KwThen     = "then"
)

// Message represents a chat message for storage.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ShadowRecord captures a student response and optional teacher response.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ShadowRecord struct {
	ID               string     `json:"id"`
	CreatedAt        time.Time  `json:"created_at"`
	ConversationID   string     `json:"conversation_id"`
	Messages         []Message  `json:"messages"`
	StudentModel     string     `json:"student_model"`
	StudentContent   string     `json:"student_content"`
	StudentTokensIn  int        `json:"student_tokens_in"`
	StudentTokensOut int        `json:"student_tokens_out"`
	TeacherModel     string     `json:"teacher_model,omitempty"`
	TeacherContent   string     `json:"teacher_content,omitempty"`
	QualityScore     float64    `json:"quality_score"`
	Preference       Preference `json:"preference"`
	Domain           Domain     `json:"domain"`
	TaskType         TaskType   `json:"task_type"`
	IsHighQuality    bool       `json:"is_high_quality"`
}

// NewShadowRecord creates a new shadow record with generated ID.
func NewShadowRecord(convID string, messages []Message, studentModel, studentContent string) *ShadowRecord {
	return &ShadowRecord{
		ID:             uuid.New().String(),
		CreatedAt:      time.Now().UTC(),
		ConversationID: convID,
		Messages:       messages,
		StudentModel:   studentModel,
		StudentContent: studentContent,
		Preference:     PreferenceTie,
		Domain:         DomainGeneral,
		TaskType:       TaskTypeChat,
	}
}

// MessagesJSON returns the messages as a JSON string.
func (r *ShadowRecord) MessagesJSON() string {
	data, err := json.Marshal(r.Messages)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// SetMessagesFromJSON parses messages from JSON.
func (r *ShadowRecord) SetMessagesFromJSON(data string) error {
	return json.Unmarshal([]byte(data), &r.Messages)
}

// HasTeacherResponse returns true if teacher response is available.
func (r *ShadowRecord) HasTeacherResponse() bool {
	return r.TeacherContent != ""
}

// PreferencePair represents a DPO training pair.
type PreferencePair struct {
	ID               string     `json:"id"`
	SourceRecordID   string     `json:"source_record_id"`
	PromptMessages   []Message  `json:"prompt_messages"`
	ChosenResponse   string     `json:"chosen_response"`
	ChosenModel      string     `json:"chosen_model"`
	RejectedResponse string     `json:"rejected_response"`
	RejectedModel    string     `json:"rejected_model"`
	Margin           float64    `json:"margin"`
	ExportedAt       *time.Time `json:"exported_at,omitempty"`
}

// NewPreferencePair creates a preference pair from a shadow record.
func NewPreferencePair(record *ShadowRecord, studentScore, teacherScore float64) *PreferencePair {
	pair := &PreferencePair{
		ID:             uuid.New().String(),
		SourceRecordID: record.ID,
		PromptMessages: record.Messages,
	}

	margin := teacherScore - studentScore

	if margin > 0 {
		// Teacher is better
		pair.ChosenResponse = record.TeacherContent
		pair.ChosenModel = record.TeacherModel
		pair.RejectedResponse = record.StudentContent
		pair.RejectedModel = record.StudentModel
		pair.Margin = margin
	} else {
		// Student is better or tie
		pair.ChosenResponse = record.StudentContent
		pair.ChosenModel = record.StudentModel
		pair.RejectedResponse = record.TeacherContent
		pair.RejectedModel = record.TeacherModel
		pair.Margin = -margin
	}

	return pair
}

// PromptJSON returns the prompt messages as JSON.
func (p *PreferencePair) PromptJSON() string {
	data, err := json.Marshal(p.PromptMessages)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// SetPromptFromJSON parses prompt messages from JSON.
func (p *PreferencePair) SetPromptFromJSON(data string) error {
	return json.Unmarshal([]byte(data), &p.PromptMessages)
}

// FewShotExample represents a high-quality example for in-context learning.
type FewShotExample struct {
	ID                string    `json:"id"`
	SourceRecordID    string    `json:"source_record_id"`
	Domain            Domain    `json:"domain"`
	TaskType          TaskType  `json:"task_type"`
	UserMessage       string    `json:"user_message"`
	AssistantResponse string    `json:"assistant_response"`
	QualityScore      float64   `json:"quality_score"`
	UsageCount        int       `json:"usage_count"`
	CreatedAt         time.Time `json:"created_at"`
	EmbeddingJSON     string    `json:"embedding_json,omitempty"`
}

// NewFewShotExample creates a few-shot example from a shadow record.
func NewFewShotExample(record *ShadowRecord) *FewShotExample {
	// Get the last user message
	var userMsg string
	for _, v := range slices.Backward(record.Messages) {
		if v.Role == RoleUser {
			userMsg = v.Content
			break
		}
	}

	// Use the best response (teacher if available and better, else student)
	response := record.StudentContent
	if record.HasTeacherResponse() && record.Preference == PreferenceTeacher {
		response = record.TeacherContent
	}

	return &FewShotExample{
		ID:                uuid.New().String(),
		SourceRecordID:    record.ID,
		Domain:            record.Domain,
		TaskType:          record.TaskType,
		UserMessage:       userMsg,
		AssistantResponse: response,
		QualityScore:      record.QualityScore,
		UsageCount:        0,
		CreatedAt:         time.Now().UTC(),
	}
}

// Adapter represents a trained LoRA/soft-prompt adapter.
type Adapter struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ModelBase        string    `json:"model_base"`
	AdapterType      string    `json:"adapter_type"` // "lora", "soft_prompt"
	AdapterPath      string    `json:"adapter_path"`
	SourceTrainingDB string    `json:"source_training_db"`
	TrainingRecords  int       `json:"training_records"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
}

// NewAdapter creates a new adapter record.
func NewAdapter(name, modelBase, adapterType, path string) *Adapter {
	return &Adapter{
		ID:          uuid.New().String(),
		Name:        name,
		ModelBase:   modelBase,
		AdapterType: adapterType,
		AdapterPath: path,
		IsActive:    false,
		CreatedAt:   time.Now().UTC(),
	}
}

// TrainingRun tracks a training session for reproducibility.
type TrainingRun struct {
	ID          string     `json:"id"`
	AdapterID   string     `json:"adapter_id"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	RecordsUsed int        `json:"records_used"`
	ConfigJSON  string     `json:"config_json"`
	FinalLoss   float64    `json:"final_loss"`
	EvalScore   float64    `json:"eval_score"`
}

// NewTrainingRun creates a new training run record.
func NewTrainingRun(adapterID string, config any) *TrainingRun {
	configData, err := json.Marshal(config)
	if err != nil {
		configData = []byte("{}")
	}
	return &TrainingRun{
		ID:         uuid.New().String(),
		AdapterID:  adapterID,
		StartedAt:  time.Now().UTC(),
		ConfigJSON: string(configData),
	}
}

// Complete marks the training run as completed.
func (r *TrainingRun) Complete(finalLoss, evalScore float64) {
	now := time.Now().UTC()
	r.CompletedAt = &now
	r.FinalLoss = finalLoss
	r.EvalScore = evalScore
}

// ShadowStats holds statistics about shadow training data.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ShadowStats struct {
	TotalRecords      int            `json:"total_records"`
	HighQualityCount  int            `json:"high_quality_count"`
	PreferencePairs   int            `json:"preference_pairs"`
	FewShotExamples   int            `json:"fewshot_examples"`
	RecordsByDomain   map[string]int `json:"records_by_domain"`
	RecordsByTaskType map[string]int `json:"records_by_task_type"`
	AvgQualityScore   float64        `json:"avg_quality_score"`
	TeacherQueries    int            `json:"teacher_queries_today"`
	TeacherCostToday  float64        `json:"teacher_cost_today"`
}

// NewShadowStats creates an empty stats object.
func NewShadowStats() *ShadowStats {
	return &ShadowStats{
		RecordsByDomain:   make(map[string]int),
		RecordsByTaskType: make(map[string]int),
	}
}
