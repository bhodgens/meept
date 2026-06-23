import '../services/sdk_client.dart';

/// Service for fetching and managing skills for autocomplete.
///
/// Wraps [SdkApiClient.getSkillsRaw] and caches the result so that repeated
/// slash-command autocompletion queries don't re-hit the daemon.
class SkillsService {
  final SdkApiClient _client;

  /// Cached skill summaries; null until the first successful fetch.
  List<SkillSummary>? _cache;

  SkillsService(this._client);

  /// Fetch all installed skills (returns cached value if still valid).
  Future<List<SkillSummary>> fetchSkills() async {
    final cached = _cache;
    if (cached != null) return cached;
    try {
      final raw = await _client.getSkillsRaw();
      final skills = raw.map(SkillSummary.fromJson).toList();
      _cache = skills;
      return skills;
    } catch (_) {
      // Fallback: return empty list on error
      return [];
    }
  }

  /// Search skills by query.
  Future<List<SkillSummary>> searchSkills(String query) async {
    final all = await fetchSkills();
    final lowerQuery = query.toLowerCase();
    return all
        .where((s) =>
            s.name.toLowerCase().contains(lowerQuery) ||
            s.description.toLowerCase().contains(lowerQuery))
        .toList();
  }

  /// Get skill names for autocomplete.
  Future<List<String>> getSkillNames() async {
    final skills = await fetchSkills();
    return skills.map((s) => s.name).toList();
  }
}

/// SkillSummary holds basic skill info for display.
class SkillSummary {
  final String name;
  final String description;
  final List<String> tags;
  final List<String> requires;

  SkillSummary({
    required this.name,
    required this.description,
    this.tags = const [],
    this.requires = const [],
  });

  factory SkillSummary.fromJson(Map<String, dynamic> json) {
    return SkillSummary(
      name: json['name'] ?? '',
      description: json['description'] ?? '',
      tags: (json['tags'] as List<dynamic>?)?.map((e) => e as String).toList() ?? [],
      requires: (json['requires'] as List<dynamic>?)?.map((e) => e as String).toList() ?? [],
    );
  }
}
