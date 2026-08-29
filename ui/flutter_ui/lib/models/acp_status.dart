/// Parsed GET /api/v1/acp/agents envelope.
class AcpAgentStatus {
  final String id;
  final bool enabled;
  final bool running;
  final String state;
  final int uptimeS;

  const AcpAgentStatus({
    required this.id,
    required this.enabled,
    required this.running,
    required this.state,
    required this.uptimeS,
  });

  factory AcpAgentStatus.fromJson(Map<String, dynamic> json) {
    return AcpAgentStatus(
      id: json['id'] as String? ?? '',
      enabled: json['enabled'] as bool? ?? false,
      running: json['running'] as bool? ?? false,
      state: json['state'] as String? ?? '',
      uptimeS: json['uptime_s'] as int? ?? 0,
    );
  }
}

class AcpStatusView {
  final bool enabled;
  final List<AcpAgentStatus> agents;

  const AcpStatusView({required this.enabled, required this.agents});

  static const disabled = AcpStatusView(enabled: false, agents: []);

  int get liveCount => agents.where((a) => a.running).length;

  factory AcpStatusView.fromJson(Map<String, dynamic>? json) {
    if (json == null) {
      return disabled;
    }
    final raw = json['agents'];
    final agents = <AcpAgentStatus>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map<String, dynamic>) {
          agents.add(AcpAgentStatus.fromJson(item));
        } else if (item is Map) {
          agents.add(AcpAgentStatus.fromJson(Map<String, dynamic>.from(item)));
        }
      }
    }
    return AcpStatusView(
      enabled: json['enabled'] as bool? ?? false,
      agents: agents,
    );
  }
}
