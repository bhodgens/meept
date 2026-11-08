/// IntakeStatusModel mirrors the frozen cross-surface intake.status
/// contract (internal/rpc/intake_status.go IntakeStatusView, consumed by
/// the TUI's internal/tui/intake_view.go and the `meept intake status`
/// CLI). JSON field names are identical to the RPC/HTTP surface — a
/// rename on any surface is a contract break, not a refactor.
///
/// Read-only snapshot of the [intake] config block plus the poller-job
/// state. It NEVER carries token material (presence-only posture, same
/// as `meept github status`).
class IntakeStatusModel {
  final bool enabled;
  final String watchRepo;
  final String pollInterval;
  final List<String> requiredLabels;
  final String doneLabel;
  final String defaultEmployee;
  final String gateCommand;
  final int projectNumber;
  final bool pollerScheduled;
  final String note;

  const IntakeStatusModel({
    this.enabled = false,
    this.watchRepo = '',
    this.pollInterval = '',
    this.requiredLabels = const [],
    this.doneLabel = '',
    this.defaultEmployee = '',
    this.gateCommand = '',
    this.projectNumber = 0,
    this.pollerScheduled = false,
    this.note = '',
  });

  factory IntakeStatusModel.fromJson(Map<String, dynamic> json) {
    // Defensive casts mirror the TUI's json.Unmarshal posture: a disabled
    // envelope may omit nothing (the view is flat), but a malformed
    // optional field must never throw the GUI.
    return IntakeStatusModel(
      enabled: json['enabled'] as bool? ?? false,
      watchRepo: json['watch_repo'] as String? ?? '',
      pollInterval: json['poll_interval'] as String? ?? '',
      requiredLabels:
          (json['required_labels'] as List?)?.whereType<String>().toList() ??
          const [],
      doneLabel: json['done_label'] as String? ?? '',
      defaultEmployee: json['default_employee'] as String? ?? '',
      gateCommand: json['gate_command'] as String? ?? '',
      projectNumber: (json['project_number'] as num?)?.toInt() ?? 0,
      pollerScheduled: json['poller_scheduled'] as bool? ?? false,
      note: json['note'] as String? ?? '',
    );
  }

  Map<String, dynamic> toJson() => {
    'enabled': enabled,
    'watch_repo': watchRepo,
    'poll_interval': pollInterval,
    'required_labels': requiredLabels,
    'done_label': doneLabel,
    'default_employee': defaultEmployee,
    'gate_command': gateCommand,
    'project_number': projectNumber,
    'poller_scheduled': pollerScheduled,
    'note': note,
  };
}
