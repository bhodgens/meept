// Riverpod seam for the kanban bridge (leaf 05/04). Kept inside
// features/kanban/ so the board view (05/05) can import it without this
// leaf touching the shared providers.dart registry — register the export
// there when 05/05 wires the view.

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/constants.dart';
import '../../providers/providers.dart';
import 'kanban_service.dart';

/// The sidecar bridge. Connect config comes from the same persisted
/// settings every other feature uses (host + bearer/api key via
/// StorageService; pairing and dev-key plumbing live elsewhere and this
/// service only ever accepts an injected token). The sidecar port is NOT
/// the daemon port: C7 defaults the gateway to 127.0.0.1:8790 and there is
/// no persisted override yet, so the default stands.
final kanbanServiceProvider = Provider<KanbanService>((ref) {
  final storage = ref.watch(storageProvider);
  final host = storage.getApiHost() ?? AppConstants.defaultApiHost;
  final token = storage.getApiKey();
  final service = KanbanService(
    host: host,
    port: KanbanService.defaultSidecarPort,
    bearerToken: (token == null || token.isEmpty) ? null : token,
  );
  ref.onDispose(service.dispose);
  return service;
});
