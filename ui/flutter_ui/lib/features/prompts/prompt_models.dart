/// Data models for the prompt-template editor surface.
///
/// Mirrors the HTTP API exposed by the daemon's `PromptService`:
///
/// `GET /api/v1/prompts` →
/// ```json
/// {"prompts": [{"name": "...", "tier": "...", "source_path": "...", "modified": "..."}]}
/// ```
///
/// `GET /api/v1/prompts/{path}` →
/// ```json
/// {"name": "...", "tier": "...", "source_path": "...", "modified": "...", "content": "..."}
/// ```
///
/// `POST /api/v1/prompts/validate` body `{"name": "..."?}` →
/// single:  `{"name": "...", "valid": bool, "error": "..."?}`
/// all:     `{"valid": bool, "errors": [{"name": "...", "error": "..."}], "checked": int}`
///
/// Tier values emitted by the daemon: `project`, `user`, `system`, `bundled`.
/// See `internal/services/prompt_service.go` (`PromptEntry`, `PromptDetail`).
library meept_ui.features.prompts.prompt_models;

/// One discoverable template file (metadata only — no content).
class PromptSummary {
  final String name;
  final String tier;
  final String sourcePath;
  final DateTime? modified;

  const PromptSummary({
    required this.name,
    required this.tier,
    required this.sourcePath,
    this.modified,
  });

  factory PromptSummary.fromJson(Map<String, dynamic> json) {
    return PromptSummary(
      name: json['name'] as String? ?? '',
      tier: (json['tier'] as String? ?? '').toLowerCase(),
      sourcePath: json['source_path'] as String? ?? '',
      modified: json['modified'] is String
          ? DateTime.tryParse(json['modified'] as String)
          : null,
    );
  }

  /// Whether this entry is a user-local override (the only tier that
  /// can be DELETEd via the HTTP API).
  bool get isUserTier => tier == 'user';

  /// Whether this entry lives in the project-local `.meept/prompts/` dir.
  bool get isProjectTier => tier == 'project';
}

/// A template plus its raw content. Returned by `GET /api/v1/prompts/{path}`
/// and `PUT /api/v1/prompts/{path}`.
class PromptDetail {
  final String name;
  final String tier;
  final String sourcePath;
  final DateTime? modified;
  final String content;

  const PromptDetail({
    required this.name,
    required this.tier,
    required this.sourcePath,
    this.modified,
    required this.content,
  });

  factory PromptDetail.fromJson(Map<String, dynamic> json) {
    return PromptDetail(
      name: json['name'] as String? ?? '',
      tier: (json['tier'] as String? ?? '').toLowerCase(),
      sourcePath: json['source_path'] as String? ?? '',
      modified: json['modified'] is String
          ? DateTime.tryParse(json['modified'] as String)
          : null,
      content: json['content'] as String? ?? '',
    );
  }
}

/// Request body for `POST /api/v1/prompts/validate`.
///
/// When [name] is null or empty, the daemon validates every template.
class PromptValidateRequest {
  final String? name;

  const PromptValidateRequest({this.name});

  Map<String, dynamic> toJson() {
    final body = <String, dynamic>{};
    if (name != null && name!.isNotEmpty) body['name'] = name;
    return body;
  }
}

/// Response shape for `POST /api/v1/prompts/validate`.
///
/// The daemon returns one of two shapes depending on whether the request
/// named a single template or validated all of them:
///
/// - single: `{"name": "...", "valid": bool, "error": "..."?}`
/// - all:    `{"valid": bool, "errors": [...], "checked": int}`
///
/// This model normalises both shapes. When [name] is non-empty, the result
/// describes a single template; otherwise [errors] lists every failure.
class PromptValidateResult {
  final String name;
  final bool valid;
  final String error;
  final List<PromptValidateError> errors;
  final int checked;

  const PromptValidateResult({
    this.name = '',
    required this.valid,
    this.error = '',
    this.errors = const [],
    this.checked = 0,
  });

  factory PromptValidateResult.fromJson(Map<String, dynamic> json) {
    final errorsRaw = json['errors'] as List? ?? [];
    final checkedRaw = json['checked'];
    final validRaw = json['valid'];
    return PromptValidateResult(
      name: json['name'] as String? ?? '',
      // Only trust a literal bool; coerce anything else to false.
      valid: validRaw is bool ? validRaw : false,
      error: json['error'] as String? ?? '',
      errors: errorsRaw
          .whereType<Map>()
          .map((m) => PromptValidateError.fromJson(
              Map<String, dynamic>.from(m)))
          .toList(),
      // Defend against malformed values (string, bool, etc.) — default 0.
      checked: checkedRaw is num ? checkedRaw.toInt() : 0,
    );
  }
}

/// One validation failure when validating all templates at once.
class PromptValidateError {
  final String name;
  final String error;

  const PromptValidateError({
    required this.name,
    required this.error,
  });

  factory PromptValidateError.fromJson(Map<String, dynamic> json) {
    return PromptValidateError(
      name: json['name'] as String? ?? '',
      error: json['error'] as String? ?? '',
    );
  }
}
