package builtin

// JSON Schema type constants used in tool parameter definitions.
const (
	schemaTypeObject  = "object"
	schemaTypeString  = "string"
	schemaTypeInteger = "integer"
	schemaTypeBoolean = "boolean"
	schemaTypeNumber  = "number"

	// Common property name constants used as map keys in tool definitions.
	schemaPropCommand       = "command"
	schemaPropContent       = "content"
	schemaPropCount         = "count"
	schemaPropDescription   = "description"
	schemaPropEnd           = "end"
	schemaPropEntityID      = "entity_id"
	schemaPropFilePath      = "file_path"
	schemaPropFound         = "found"
	schemaPropJobID         = "job_id"
	schemaPropJobType       = "job_type"
	schemaPropLanguage      = "language"
	schemaPropLimit         = "limit"
	schemaPropLine          = "line"
	schemaPropCharacter     = "character"
	schemaPropStartLine     = "start_line"
	schemaPropStartChar     = "start_char"
	schemaPropEndLine       = "end_line"
	schemaPropEndChar       = "end_char"
	schemaPropMessage       = "message"
	schemaPropName          = "name"
	schemaPropPath          = "path"
	schemaPropPriority      = "priority"
	schemaPropQuery         = "query"
	schemaPropStart         = "start"
	schemaPropTags          = "tags"
	schemaPropRequires      = "requires"
	schemaPropRiskLevel     = "risk_level"
	schemaPropType          = "type"
	schemaPropConversationID = "conversation_id"
	schemaPropMemoryID      = "memory_id"
	schemaPropCategory      = "category"
	schemaPropState         = "state"
	schemaPropModel         = "model"
	schemaPropSuccess       = "success"
	schemaPropStatus        = "status"

	// Scheduler error messages.
	errSchedulerNotAvailable = "scheduler not available"
	errJobIDRequired         = "job_id is required"

	// Knowledge graph relationship type.
	schemaPropReference = "reference"

	// Memory type constants.
	schemaMemoryEpisodic = "episodic"
	schemaMemoryTask     = "task"

	// Job type constants for scheduler tools.
	schemaJobTypeShell = "shell"
)
