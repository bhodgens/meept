import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/router.dart' show rootNavigatorKey;
import '../models/acp_permission.dart';
import '../providers/acp_permission_provider.dart';

/// AcpPermissionDialog renders the head of the pending ACP human-relay
/// queue (acp-human-permission-relay leaf 05, Contract E): agent id,
/// session id, created time, bounded summary, request-id short form, and
/// allow/deny buttons that POST /api/v1/acp/permissions/resolve.
///
/// Purely state-driven: the host (AcpPermissionDialogHost) mounts it only
/// while a head exists; a resolved frame (external answer, e.g. the TUI or
/// CLI, or a relay timeout) clears the queue entry and the host closes the
/// dialog. The dialog never pops itself — resolve outcomes flow through the
/// provider — matching the DestructiveConfirmationDialog shape (a plain
/// AlertDialog whose buttons report decisions back to the notifier).
class AcpPermissionDialog extends ConsumerWidget {
  final AcpPendingPermission request;

  const AcpPermissionDialog({super.key, required this.request});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(acpPermissionProvider);
    final error = state.error;
    final queueCount = state.count;
    return AlertDialog(
      key: const Key('acp-permission-dialog'),
      title: Text(
        'acp permission request${queueCount > 1 ? ' (x$queueCount)' : ''}',
      ),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              request.summary.isNotEmpty
                  ? request.boundedSummary
                  : 'no summary',
            ),
            const SizedBox(height: 16),
            _FieldLine(label: 'agent', value: request.agentId),
            _FieldLine(label: 'session', value: request.sessionId),
            _FieldLine(label: 'created', value: request.createdDisplay),
            _FieldLine(label: 'request', value: request.shortId),
            if (queueCount > 1) ...[
              const SizedBox(height: 8),
              Text(
                '${queueCount - 1} more queued',
                style: TextStyle(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (error != null) ...[
              const SizedBox(height: 8),
              Text(
                'resolve failed: $error',
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          key: const Key('acp-permission-deny'),
          onPressed: () => ref
              .read(acpPermissionProvider.notifier)
              .resolve(request.id, 'deny'),
          child: const Text('deny'),
        ),
        FilledButton(
          key: const Key('acp-permission-allow'),
          onPressed: () => ref
              .read(acpPermissionProvider.notifier)
              .resolve(request.id, 'allow'),
          child: const Text('allow'),
        ),
      ],
    );
  }
}

class _FieldLine extends StatelessWidget {
  final String label;
  final String value;

  const _FieldLine({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Text(
        '$label: ${value.isEmpty ? '-' : value}',
        style: const TextStyle(fontFamily: 'SourceCodePro', fontSize: 12),
      ),
    );
  }
}

/// Hosts the dialog app-wide: while the pending queue has a head, the head
/// is pushed as a dialog route on [navigatorKey]; the head swapping
/// (local resolve, external answer, timeout) closes it and the next head
/// opens. Mounted by the MaterialApp builder above the Navigator, so route
/// push/pop goes through the navigator key rather than a context lookup
/// (there is no Navigator above this widget's own context).
///
/// Queue-count surfacing: the dialog title carries the stack counter
/// ("acp permission request (xN)") and the StatusBar shows a
/// "<N> acp permissions pending" span (precedent: the pending-changes
/// warning span) — the count is visible without stacking dialogs.
class AcpPermissionDialogHost extends ConsumerStatefulWidget {
  final GlobalKey<NavigatorState>? navigatorKey;

  const AcpPermissionDialogHost({super.key, this.navigatorKey});

  GlobalKey<NavigatorState> get _key => navigatorKey ?? rootNavigatorKey;

  @override
  ConsumerState<AcpPermissionDialogHost> createState() =>
      _AcpPermissionDialogHostState();
}

class _AcpPermissionDialogHostState
    extends ConsumerState<AcpPermissionDialogHost> {
  /// Id of the head the currently visible dialog route was pushed for
  /// (null when no dialog is shown). The same head must not re-push; a
  /// different head swaps the dialog.
  String? _shownId;

  @override
  Widget build(BuildContext context) {
    ref.listen<AcpPermissionState>(acpPermissionProvider, (previous, next) {
      _syncDialog(next);
    });
    // First frame: the initial GET catch-up may land a head before any
    // listener callback fires (data can already be resolved at mount).
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _syncDialog(ref.read(acpPermissionProvider));
    });
    return const SizedBox.shrink();
  }

  void _syncDialog(AcpPermissionState state) {
    final navigator = widget._key.currentState;
    if (navigator == null) return; // router not mounted (pairing screen)
    final head = state.head;
    if (head == null) {
      if (_shownId != null) {
        _close(navigator);
      }
      return;
    }
    if (_shownId == head.id) {
      // Same head, possibly a deeper queue now — the dialog itself
      // re-renders via its provider watch; nothing to do here.
      return;
    }
    if (_shownId != null) {
      // Previous head cleared out-of-band; swap to the new one.
      _close(navigator);
    }
    _shownId = head.id;
    showDialog<void>(
      context: navigator.context,
      barrierDismissible: false,
      builder: (_) => AcpPermissionDialog(request: head),
    ).then((_) {
      // If THIS dialog's route was popped, forget it so the next head can
      // open a fresh dialog.
      if (mounted && _shownId == head.id) {
        _shownId = null;
      }
    });
  }

  void _close(NavigatorState navigator) {
    _shownId = null;
    if (navigator.canPop()) {
      navigator.pop();
    }
  }
}
