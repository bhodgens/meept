import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Per-session find bar visibility.
final findBarVisibleProvider =
    StateProvider.family<bool, String>((ref, sessionId) => false);

/// Per-session find query.
final findQueryProvider =
    StateProvider.family<String, String>((ref, sessionId) => '');

/// Per-session case-sensitive toggle.
final findCaseSensitiveProvider =
    StateProvider.family<bool, String>((ref, sessionId) => false);

/// Per-session regex toggle.
final findRegexProvider =
    StateProvider.family<bool, String>((ref, sessionId) => false);

/// Per-session cursor (which match is "current", 0-based index into the flat
/// list of all matches across all messages).
final findCursorProvider =
    StateProvider.family<int, String>((ref, sessionId) => 0);

/// A single match location.
class FindMatch {
  final int messageIndex;
  final int start;
  final int end;
  const FindMatch({
    required this.messageIndex,
    required this.start,
    required this.end,
  });
}

/// Computes find matches for a session given the current query, messages,
/// and toggle states. Result is a flat list; cursor indexes into it.
class FindMatches {
  final List<FindMatch> matches;
  final String? regexError;
  const FindMatches({required this.matches, this.regexError});

  static const empty = FindMatches(matches: []);
}

/// Computes find matches over the given content list.
///
/// Returns a [FindMatches] containing all match locations (as pairs of
/// messageIndex + byte offsets) plus any regex compile error.
FindMatches computeFindMatches({
  required List<String> contents,
  required String query,
  required bool caseSensitive,
  required bool regex,
  int maxMatches = 1000,
}) {
  if (query.isEmpty) {
    return FindMatches.empty;
  }

  final List<FindMatch> matches = [];

  if (regex) {
    try {
      final pattern = caseSensitive
          ? RegExp(query)
          : RegExp(query, caseSensitive: false);
      for (var i = 0; i < contents.length && matches.length < maxMatches; i++) {
        for (final m in pattern.allMatches(contents[i])) {
          matches.add(FindMatch(messageIndex: i, start: m.start, end: m.end));
          if (matches.length >= maxMatches) break;
        }
      }
    } on FormatException catch (e) {
      return FindMatches(matches: const [], regexError: e.message);
    }
    return FindMatches(matches: matches);
  }

  final needle = caseSensitive ? query : query.toLowerCase();
  for (var i = 0; i < contents.length; i++) {
    final raw = contents[i];
    final hay = caseSensitive ? raw : raw.toLowerCase();
    if (needle.isEmpty) continue;
    var start = 0;
    while (true) {
      final idx = hay.indexOf(needle, start);
      if (idx < 0) break;
      final end = idx + needle.length;
      matches.add(FindMatch(messageIndex: i, start: idx, end: end));
      if (matches.length >= maxMatches) break;
      start = end;
    }
  }
  return FindMatches(matches: matches);
}
