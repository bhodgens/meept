package security

// Tool action name constants.
const (
	ActionShellExecute = "shell_execute"
	ActionFileRead     = "file_read"
	ActionFileWrite    = "file_write"
	ActionFileDelete   = "file_delete"

	// Decision types.
	DecisionAllow = "allow"
	DecisionBlock = "block"

	// Rule sources.
	RuleSourceImmutable   = "immutable"
	RuleSourceFailClosed  = "fail_closed"

	// Category names for pattern rules.
	CategoryDestructive     = "destructive"
	CategorySelfReplication = "self_replication"
	CategorySystem          = "system"
	CategoryInstall         = "install"
	CategoryCodeExecution   = "code_execution"
	CategoryVCS             = "vcs"
	CategoryBuild           = "build"

	// Detection labels.
	LabelInstructionOverride = "instruction_override"
	LabelSpecialTokenPhi    = "special_token_phi"
	LabelSpecialToken       = "special_token"

	// Pattern type constants.
	PatternTypeRegex = "regex"

	// Eval detection pattern.
	PatternEval = "eval "

	// Binary name.
	BinaryTirith = "tirith"

	// Error reasons.
	ReasonPathRuleQueryFailed = "path rule query failed"

	// System role.
	RoleSystem = "system"

	// Prompt pattern types.
	TypeInstructionOverride = "instruction_override"
)
