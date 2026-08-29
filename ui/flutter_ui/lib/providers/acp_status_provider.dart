import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/acp_status.dart';
import 'providers.dart';

/// Fetches GET /api/v1/acp/agents. Failures and missing daemon map to
/// [AcpStatusView.disabled] so the status bar stays empty by default.
final acpStatusProvider = FutureProvider<AcpStatusView>((ref) async {
  final client = ref.watch(sdkClientProvider);
  try {
    final raw = await client.getAcpAgents();
    return AcpStatusView.fromJson(raw);
  } catch (_) {
    return AcpStatusView.disabled;
  }
});
