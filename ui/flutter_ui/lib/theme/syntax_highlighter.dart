import 'package:flutter/material.dart';
import 'package:flutter_highlight/themes/atom-one-dark.dart';
import 'package:highlight/highlight.dart' show highlight;
import 'package:flutter_markdown/flutter_markdown.dart';

/// Syntax highlighter adapter using the highlight core package.
///
/// Converts parsed code into colorized TextSpans matching the atom-one-dark
/// theme.  This is compatible with flutter_markdown's `syntaxHighlighter`.
class CyberpunkSyntaxHighlighter extends SyntaxHighlighter {
  final String? language;

  CyberpunkSyntaxHighlighter({this.language});

  @override
  TextSpan format(String source) {
    final nodes = highlight.parse(source, language: language).nodes;
    if (nodes == null) {
      return TextSpan(
        text: source,
        style: atomOneDarkTheme['root'],
      );
    }
    return TextSpan(
      style: atomOneDarkTheme['root'],
      children: _convertNodes(nodes, atomOneDarkTheme),
    );
  }

  /// Recursive traversal of highlight parse nodes → TextSpan tree.
  List<TextSpan> _convertNodes(
    List<dynamic> nodes,
    Map<String, TextStyle> theme,
  ) {
    final spans = <TextSpan>[];
    for (final node in nodes) {
      if (node.value != null) {
        final style = node.className == null
            ? null
            : theme[node.className!];
        spans.add(TextSpan(text: node.value, style: style));
      } else if (node.children != null) {
        final childStyle = node.className == null
            ? null
            : theme[node.className!];
        spans.add(
          TextSpan(
            style: childStyle,
            children: _convertNodes(node.children!, theme),
          ),
        );
      }
    }
    return spans;
  }
}
