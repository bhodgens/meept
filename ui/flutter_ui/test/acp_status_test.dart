import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/acp_status.dart';

void main() {
  test('disabled envelope', () {
    final v = AcpStatusView.fromJson({'enabled': false, 'agents': []});
    expect(v.enabled, isFalse);
    expect(v.liveCount, 0);
  });

  test('null json is disabled', () {
    expect(AcpStatusView.fromJson(null).enabled, isFalse);
  });

  test('live count counts running agents', () {
    final v = AcpStatusView.fromJson({
      'enabled': true,
      'agents': [
        {
          'id': 'codex',
          'enabled': true,
          'running': true,
          'state': 'ready',
          'uptime_s': 0,
        },
        {
          'id': 'other',
          'enabled': true,
          'running': false,
          'state': 'closed',
          'uptime_s': 0,
        },
      ],
    });
    expect(v.enabled, isTrue);
    expect(v.liveCount, 1);
    expect(v.agents.first.id, 'codex');
  });
}
