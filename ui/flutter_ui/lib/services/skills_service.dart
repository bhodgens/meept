import 'package:http/http.dart' as http;
import 'dart:convert';
import '../models/skill.dart';
import '../core/slash_commands.dart';

/// Service for fetching and managing skills.
class SkillsService {
  final http.Client _client;
  final String _baseUrl;

  SkillsService({http.Client? client, String baseUrl = 'http://localhost:8081'})
      : _client = client ?? http.Client(),
        _baseUrl = baseUrl;

  /// Fetch all installed skills.
  Future<List<SkillSummary>> fetchSkills() async {
    try {
      final response = await _client.get(
        Uri.parse('$_baseUrl/api/v1/skills'),
      );
      if (response.statusCode == 200) {
        final data = jsonDecode(response.body) as List;
        return data.map((s) => SkillSummary.fromJson(s)).toList();
      }
      throw Exception('Failed to fetch skills: ${response.statusCode}');
    } catch (e) {
      // Fallback: return empty list on error
      return [];
    }
  }

  /// Search skills by query.
  Future<List<SkillSummary>> searchSkills(String query) async {
    final all = await fetchSkills();
    final lowerQuery = query.toLowerCase();
    return all.where((s) =>
        s.name.toLowerCase().contains(lowerQuery) ||
        s.description.toLowerCase().contains(lowerQuery)).toList();
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
