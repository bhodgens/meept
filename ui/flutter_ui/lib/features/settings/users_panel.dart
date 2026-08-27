import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/constants.dart';
import '../../providers/providers.dart';
import '../../services/sdk_client.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Multi-user awareness section for the settings panel (client-tooling leaf).
///
/// Parity rule with the TUI users view: both surfaces offer the identical
/// capability set. v1 capability set = awareness only:
///
/// - reports whether multi-user auth is enabled, read live from the daemon
///   config (`multiuser.enabled`, fetched via GET /api/v1/config/memory)
/// - points every management action at the `meept users` CLI on the daemon
///   host — the daemon exposes no client-callable user-management path yet
///   (no /api/v1/users routes; even the filesystem surface is directories
///   only, so the users store cannot be listed remotely).
///
/// Disabled empty state points at `multiuser.enabled` per spec. Raw keys can
/// never leak here: nothing key-shaped is ever displayed, and the store
/// persists sha256 hashes only anyway.
class UsersPanel extends ConsumerStatefulWidget {
  const UsersPanel({super.key});

  @override
  ConsumerState<UsersPanel> createState() => _UsersPanelState();
}

class _UsersPanelState extends ConsumerState<UsersPanel> {
  late final SdkApiClient _client;
  Timer? _refreshTimer;

  bool _isLoading = true;
  // null while unknown (config fetch failed); otherwise reflects
  // multiuser.enabled from the live daemon config.
  bool? _multiUserEnabled;

  @override
  void initState() {
    super.initState();
    _client = ref.read(sdkClientProvider);
    _load();
    // Poll lightly so toggling multiuser.enabled shows up without reopening
    // settings. Cheap: one small read-only GET.
    _refreshTimer = Timer.periodic(const Duration(seconds: 30), (_) => _load());
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final raw = await _client.getMemoryConfig();
      if (!mounted) return;
      final cfg = _decodeJson5ish(raw);
      final mu = cfg['multiuser'];
      setState(() {
        _isLoading = false;
        _multiUserEnabled = mu is Map && mu['enabled'] == true;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _isLoading = false;
        _multiUserEnabled = null;
      });
    }
  }

  /// Conservative JSON5 decode mirroring parseClientConfig's fallbacks,
  /// trimmed to what meept.json5 needs (comments + trailing commas).
  static Map<String, dynamic> _decodeJson5ish(String raw) {
    Map<String, dynamic> tryDecode(String s) {
      final v = jsonDecode(s);
      if (v is Map<String, dynamic>) return v;
      if (v is Map) return v.map((k, val) => MapEntry('$k', val));
      throw const FormatException('not an object');
    }

    var cleaned = raw
        .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
        .replaceAll(RegExp(r'//[^\n]*'), '');
    cleaned = cleaned.replaceAllMapped(
      RegExp(r',\s*([}\]])'),
      (m) => m.group(1)!,
    );
    return tryDecode(cleaned);
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.people_alt, color: CyberpunkColors.blueInfo, size: 16),
              const SizedBox(width: 8),
              Text(
                'users',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.blueInfo,
                ),
              ),
              const SizedBox(width: 8),
              if (_isLoading)
                SizedBox(
                  width: 12,
                  height: 12,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      CyberpunkColors.orangePrimary,
                    ),
                  ),
                )
              else ...[
                Text(
                  switch (_multiUserEnabled) {
                    true => 'multi-user: on',
                    false => 'multi-user: off',
                    null => 'multi-user: unknown',
                  },
                  style: CyberpunkTypography.bodySmall.copyWith(
                    fontFamily: 'SourceCodePro',
                    fontSize: 10,
                    color: switch (_multiUserEnabled) {
                      true => CyberpunkColors.greenSuccess,
                      false => CyberpunkColors.midGray,
                      null => CyberpunkColors.redAlert,
                    },
                  ),
                ),
                IconButton(
                  visualDensity: VisualDensity.compact,
                  iconSize: 14,
                  tooltip: 'refresh',
                  icon: Icon(Icons.refresh,
                      size: 14, color: CyberpunkColors.orangePrimary),
                  onPressed: _load,
                ),
              ],
            ],
          ),
          const SizedBox(height: 4),
          _buildBody(),
        ],
      ),
    );
  }

  Widget _buildBody() {
    // Config fetch failed — say so rather than implying anything about mode.
    if (!_isLoading && _multiUserEnabled == null) {
      return Text(
        'could not load daemon config to check multi-user state',
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.redAlert,
        ),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Spec-mandated disabled empty state when off.
        if (_multiUserEnabled == false)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Text(
              'multi-user is disabled — set multiuser.enabled = true in '
              'meept.json5 and restart the daemon',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontStyle: FontStyle.italic,
              ),
            ),
          )
        else
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Text(
              'multi-user auth is active: clients authenticate per user/key '
              '(ids, labels, expiry managed server-side)',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
            ),
          ),
        Text(
          '// manage via cli on the daemon host:',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
            fontFamily: 'SourceCodePro',
            fontSize: 10,
          ),
        ),
        Text(
          AppConstants.defaultCliGuidance,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.lightGray,
            fontFamily: 'SourceCodePro',
            fontSize: 10,
          ),
        ),
      ],
    );
  }
}
