import 'package:flutter/material.dart';

import 'board_view.dart';

/// Kanban tab — GUI twin of the TUI's kanban panel (ViewKanban, key 'b',
/// internal/tui/kanban_view.go). Structure mirrors features/intake/
/// intake_tab.dart: a bordered container around the view widget.
class KanbanTab extends StatelessWidget {
  const KanbanTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        border: Border(left: BorderSide(color: Colors.black26, width: 1)),
      ),
      child: const KanbanBoardView(),
    );
  }
}
