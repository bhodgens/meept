// One card on the kanban board (leaf 05/05). Shows title, work-ref kind,
// employee, labels — and the tap-to-move affordance: a "move" menu of the
// other columns (drag-and-drop was deliberately not implemented; the leaf
// spec allows either and menu-based moves stay flake-free in widget
// tests). Static data, no service access; the parent wires callbacks.

import 'package:flutter/material.dart';

import 'kanban_models.dart';

class KanbanCardTile extends StatelessWidget {
  const KanbanCardTile({
    super.key,
    required this.card,
    required this.moveTargets,
    required this.onMove,
    required this.onOpenDetail,
  });

  final KanbanCard card;

  /// Columns a human could move this card to (every column but its own).
  final List<KanbanColumn> moveTargets;

  /// Called with the chosen target column's id.
  final void Function(String targetColumnId) onMove;

  /// Tapping the card body opens the detail sheet (timeline + comment).
  final VoidCallback onOpenDetail;

  @override
  Widget build(BuildContext context) {
    final meta = <String>[
      if (!card.ref.isEmpty) card.ref.kind.toLowerCase(),
      if (card.employeeId.isNotEmpty) 'emp ${card.employeeId}',
    ];
    return Card(
      color: const Color(0xFF2A2A31),
      margin: const EdgeInsets.only(bottom: 6),
      child: InkWell(
        key: ValueKey('kanban-card-${card.id}'),
        onTap: onOpenDetail,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(10, 8, 6, 8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                card.title,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 4),
              Wrap(
                spacing: 6,
                runSpacing: 2,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  for (final m in meta)
                    Text(
                      m,
                      style: const TextStyle(
                        color: Colors.white54,
                        fontSize: 11,
                      ),
                    ),
                  for (final label in card.labels)
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 1,
                      ),
                      decoration: BoxDecoration(
                        color: Colors.white12,
                        borderRadius: BorderRadius.circular(6),
                      ),
                      child: Text(
                        label.toLowerCase(),
                        style: const TextStyle(
                          color: Colors.white70,
                          fontSize: 10,
                        ),
                      ),
                    ),
                ],
              ),
              if (moveTargets.isNotEmpty)
                Align(
                  alignment: Alignment.centerRight,
                  child: PopupMenuButton<String>(
                    key: ValueKey('kanban-move-${card.id}'),
                    tooltip: 'move',
                    enabled: true,
                    icon: const Icon(
                      Icons.open_with,
                      size: 16,
                      color: Colors.white38,
                    ),
                    itemBuilder: (context) => [
                      for (final target in moveTargets)
                        PopupMenuItem<String>(
                          value: target.id,
                          child: Text(
                            target.name.toLowerCase(),
                            style: const TextStyle(
                              color: Colors.white70,
                              fontSize: 12,
                            ),
                          ),
                        ),
                    ],
                    onSelected: onMove,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
