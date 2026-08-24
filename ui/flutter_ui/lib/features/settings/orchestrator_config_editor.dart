import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import '../../services/sdk_client.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Field metadata for the orchestrator config editor. Mirrors
/// `internal/config/schema.go` OrchestratorConfig (json tags).
class _OrchestratorField {
  final String key;
  final String label;
  final String hint;
  final _FieldType type;
  const _OrchestratorField({
    required this.key,
    required this.label,
    this.hint = '',
    this.type = _FieldType.int,
  });
}

enum _FieldType { int, double, bool }

const _orchestratorFields = <_OrchestratorField>[
  _OrchestratorField(
    key: 'max_plan_steps',
    label: 'max plan steps',
    hint: 'planner step cap',
  ),
  _OrchestratorField(key: 'max_research_steps', label: 'max research steps'),
  _OrchestratorField(key: 'planner_timeout', label: 'planner timeout (s)'),
  _OrchestratorField(key: 'token_budget_alert', label: 'token budget alert'),
  _OrchestratorField(key: 'max_handoff_steps', label: 'max handoff steps'),
  _OrchestratorField(
    key: 'handoff_use_amendment',
    label: 'handoff uses amendment',
    type: _FieldType.bool,
  ),
  _OrchestratorField(
    key: 'ambiguity_threshold',
    label: 'ambiguity threshold',
    type: _FieldType.double,
  ),
  _OrchestratorField(
    key: 'interview_ambiguity_threshold',
    label: 'interview ambiguity threshold',
    type: _FieldType.double,
  ),
  _OrchestratorField(key: 'max_steps_per_phase', label: 'max steps per phase'),
  _OrchestratorField(key: 'max_phases', label: 'max phases'),
];

/// Structured editor for the daemon's orchestrator block in meept.json5.
///
/// Loads via GET /api/v1/config/orchestrator and saves the whole typed
/// block via PUT /api/v1/config/orchestrator (daemon merges it back
/// atomically, preserving all other top-level keys).
class OrchestratorConfigEditor extends ConsumerStatefulWidget {
  const OrchestratorConfigEditor({super.key});

  @override
  ConsumerState<OrchestratorConfigEditor> createState() =>
      _OrchestratorConfigEditorState();
}

class _OrchestratorConfigEditorState
    extends ConsumerState<OrchestratorConfigEditor> {
  late final SdkApiClient _client;
  final Map<String, TextEditingController> _controllers = {};
  final Map<String, bool> _boolValues = {};

  bool _isLoading = true;
  bool _isSaving = false;
  bool _hasChanges = false;
  String? _error;
  Map<String, dynamic> _original = {};

  @override
  void initState() {
    super.initState();
    _client = ref.read(sdkClientProvider);
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final oc = await _client.getOrchestratorConfig();
      if (!mounted) return;
      _original = oc;
      for (final f in _orchestratorFields) {
        final v = oc[f.key];
        switch (f.type) {
          case _FieldType.bool:
            _boolValues[f.key] = v is bool ? v : false;
          case _FieldType.double:
            _controllers[f.key]?.dispose();
            _controllers[f.key] = TextEditingController(
              text: v == null ? '' : v.toString(),
            );
          case _FieldType.int:
            _controllers[f.key]?.dispose();
            _controllers[f.key] = TextEditingController(
              text: v == null ? '' : v.toString(),
            );
        }
      }
      setState(() {
        _isLoading = false;
        _hasChanges = false;
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  void _markChanged() {
    if (!_hasChanges) setState(() => _hasChanges = true);
  }

  Map<String, dynamic>? _buildPayload() {
    final payload = Map<String, dynamic>.from(_original);
    for (final f in _orchestratorFields) {
      if (f.type == _FieldType.bool) {
        payload[f.key] = _boolValues[f.key] ?? false;
        continue;
      }
      final text = _controllers[f.key]?.text.trim() ?? '';
      if (text.isEmpty) continue;
      if (f.type == _FieldType.int) {
        final v = int.tryParse(text);
        if (v == null) return null; // invalid input — block save
        payload[f.key] = v;
      } else {
        final v = double.tryParse(text);
        if (v == null) return null;
        payload[f.key] = v;
      }
    }
    return payload;
  }

  Future<void> _save() async {
    final payload = _buildPayload();
    if (payload == null) {
      setState(() => _error = 'invalid numeric value in one or more fields');
      return;
    }
    setState(() {
      _isSaving = true;
      _error = null;
    });
    try {
      await _client.saveOrchestratorConfig(payload);
      if (!mounted) return;
      setState(() {
        _isSaving = false;
        _hasChanges = false;
        _original = payload;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: const Text('orchestrator config saved'),
          backgroundColor: CyberpunkColors.greenSuccess,
          duration: const Duration(seconds: 2),
        ),
      );
    } catch (e) {
      if (mounted) {
        setState(() {
          _isSaving = false;
          _error = e.toString();
        });
      }
    }
  }

  @override
  void dispose() {
    for (final c in _controllers.values) {
      c.dispose();
    }
    super.dispose();
  }

  InputDecoration _decoration(String label, String hint, IconData icon) =>
      InputDecoration(
        labelText: label,
        labelStyle: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.lightGray,
        ),
        hintText: hint,
        prefixIcon: Icon(icon, color: CyberpunkColors.orangePrimary, size: 18),
        isDense: true,
        filled: true,
        fillColor: CyberpunkColors.black,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(6),
          borderSide: BorderSide(color: CyberpunkColors.midGray),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(6),
          borderSide: BorderSide(color: CyberpunkColors.midGray),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(6),
          borderSide: BorderSide(
            color: CyberpunkColors.orangePrimary,
            width: 1.5,
          ),
        ),
        errorStyle: TextStyle(color: CyberpunkColors.redAlert, fontSize: 10),
      );

  Widget _field(_OrchestratorField f) {
    if (f.type == _FieldType.bool) {
      return SwitchListTile(
        dense: true,
        contentPadding: EdgeInsets.zero,
        title: Text(
          f.label,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.lightGray,
          ),
        ),
        value: _boolValues[f.key] ?? false,
        activeThumbColor: CyberpunkColors.orangePrimary,
        onChanged: (v) {
          setState(() => _boolValues[f.key] = v);
          _markChanged();
        },
      );
    }
    return TextField(
      controller: _controllers[f.key],
      keyboardType: TextInputType.numberWithOptions(
        decimal: f.type == _FieldType.double,
      ),
      style: CyberpunkTypography.bodySmall.copyWith(
        fontFamily: 'SourceCodePro',
      ),
      onChanged: (_) => _markChanged(),
      decoration: _decoration(f.label, f.hint, Icons.tune),
    );
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
              Icon(
                Icons.account_tree,
                color: CyberpunkColors.blueInfo,
                size: 16,
              ),
              const SizedBox(width: 8),
              Text(
                'orchestrator',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.blueInfo,
                ),
              ),
              const Spacer(),
              if (_isSaving)
                SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      CyberpunkColors.black,
                    ),
                  ),
                )
              else if (_hasChanges)
                ElevatedButton.icon(
                  onPressed: _save,
                  icon: const Icon(Icons.save, size: 14),
                  label: const Text('save'),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: CyberpunkColors.greenSuccess,
                    foregroundColor: CyberpunkColors.black,
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            '// orchestrator block in ~/.meept/meept.json5 — saved atomically, other keys preserved',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
              fontSize: 10,
            ),
          ),
          const SizedBox(height: 12),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                'error: $_error',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                ),
              ),
            ),
          if (_isLoading)
            Center(
              child: SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  valueColor: AlwaysStoppedAnimation<Color>(
                    CyberpunkColors.orangePrimary,
                  ),
                ),
              ),
            )
          else
            ..._orchestratorFields.map(_field),
        ],
      ),
    );
  }
}
