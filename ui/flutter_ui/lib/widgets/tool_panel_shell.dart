import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../providers/providers.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// Leave the currently open tool panel and return to the chat view.
///
/// A tool panel opens through one of two paths:
///   1. a full-screen route (`/tools/<name>`), or
///   2. the embedded chat-tab tool slot (`activeToolProvider`).
///
/// Both paths are cleared here, so one helper is correct for either
/// path. A pushed detail page (for example a prompt detail) pops
/// first, because `context.canPop()` is true in that case.
void exitToolPanel(BuildContext context, WidgetRef ref) {
  ref.read(activeToolProvider.notifier).state = '';
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/');
  }
}

/// Shared chrome for every tool panel.
///
/// Gives each panel the same three things (AGENTS.md: TUI and GUI
/// features stay at parity, UI text stays lowercase):
///   - a back control that returns to the chat view,
///   - an ESC shortcut with the same effect,
///   - the panel body.
///
/// Wrap the whole panel body in this shell. Do not add a second,
/// panel-local back button: one shared control keeps the exit
/// behavior identical everywhere.
///
/// ESC is handled at this ancestor node, so an inner widget that
/// handles ESC itself (the find bar, the command palette) keeps
/// priority: key events reach the primary focus first and stop at
/// the first handler that returns `handled`.
class ToolPanelShell extends ConsumerWidget {
  /// Panel name shown in the header, lowercase.
  final String title;

  /// Optional icon shown between the back control and the title.
  final IconData? icon;

  /// Optional widgets shown at the trailing edge of the header row.
  final List<Widget> actions;

  /// Optional extra controls rendered under the shared header row,
  /// inside the same bordered block (for example a panel search
  /// field, a tab bar, or a filter row).
  final Widget? header;

  /// Panel body.
  final Widget child;

  const ToolPanelShell({
    super.key,
    required this.title,
    required this.child,
    this.icon,
    this.actions = const <Widget>[],
    this.header,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Focus(
      // Autofocus so ESC reaches this node without a click first.
      autofocus: true,
      onKeyEvent: (node, event) {
        if (event is KeyDownEvent &&
            event.logicalKey == LogicalKeyboardKey.escape) {
          exitToolPanel(context, ref);
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _buildHeader(context, ref),
          Expanded(child: child),
        ],
      ),
    );
  }

  Widget _buildHeader(BuildContext context, WidgetRef ref) {
    return Container(
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Row(
              children: [
                IconButton(
                  icon: const Icon(Icons.arrow_back, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  tooltip: 'back (esc)',
                  onPressed: () => exitToolPanel(context, ref),
                ),
                if (icon != null) ...[
                  const SizedBox(width: 4),
                  Icon(icon, color: CyberpunkColors.orangePrimary, size: 18),
                ],
                const SizedBox(width: 8),
                Text(
                  title.toLowerCase(),
                  style: CyberpunkTypography.label.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                ...actions,
              ],
            ),
          ),
          if (header != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 10),
              child: header!,
            ),
        ],
      ),
    );
  }
}
