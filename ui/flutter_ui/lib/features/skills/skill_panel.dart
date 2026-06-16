import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';

/// SkillPanel displays available skills and allows skill execution.
///
/// Fetches the skill list from the backend API. When a skill is tapped,
/// fetches its UI descriptor and renders a dynamic form. The form fields
/// are built from the descriptor's `form_fields` array. Submitting the
/// form executes the skill via `POST /api/v1/skills/{slug}/execute`.
class SkillPanel extends ConsumerStatefulWidget {
  /// Optional slug to pre-select a specific skill.
  final String? initialSlug;

  const SkillPanel({super.key, this.initialSlug});

  @override
  ConsumerState<SkillPanel> createState() => _SkillPanelState();
}

class _SkillPanelState extends ConsumerState<SkillPanel> {
  List<Skill> _skills = [];
  bool _isLoading = true;
  String? _error;

  // Selected skill + UI descriptor
  Skill? _selectedSkill;
  SkillUiDescriptor? _uiDescriptor;
  bool _isLoadingUi = false;
  bool _isExecuting = false;
  String? _uiError;

  // Form controllers keyed by field name
  final Map<String, TextEditingController> _textControllers = {};
  final Map<String, bool> _boolValues = {};
  final Map<String, String?> _selectValues = {};

  // Execution result
  SkillExecuteResult? _executeResult;

  @override
  void initState() {
    super.initState();
    _loadSkills();
  }

  @override
  void dispose() {
    for (final c in _textControllers.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _loadSkills() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final client = ref.read(apiClientProvider);
      final skills = await client.getSkills();
      if (mounted) {
        setState(() {
          _skills = skills.where((s) => s.enabled).toList();
          _isLoading = false;
          _error = null;
        });
        // If an initial slug was given, select it
        if (widget.initialSlug != null) {
          _selectSkill(widget.initialSlug!);
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  void _selectSkill(String slug) {
    final skill = _skills.where((s) => s.slug == slug).firstOrNull;
    if (skill == null) return;
    _clearFormControllers();
    setState(() {
      _selectedSkill = skill;
      _uiDescriptor = null;
      _executeResult = null;
      _uiError = null;
      _isLoadingUi = true;
    });
    _loadSkillUi(skill.slug);
  }

  Future<void> _loadSkillUi(String slug) async {
    try {
      final client = ref.read(apiClientProvider);
      final descriptor = await client.getSkillUi(slug);
      if (mounted) {
        // Pre-populate form controllers from descriptor defaults
        _initFormControllers(descriptor);
        setState(() {
          _uiDescriptor = descriptor;
          _isLoadingUi = false;
          _uiError = null;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoadingUi = false;
          _uiError = e.toString();
        });
      }
    }
  }

  void _initFormControllers(SkillUiDescriptor descriptor) {
    for (final field in descriptor.formFields) {
      if (field.type == 'boolean') {
        _boolValues[field.name] = field.defaultValue == 'true';
      } else if (field.type == 'select') {
        _selectValues[field.name] = field.defaultValue;
      } else {
        _textControllers[field.name] = TextEditingController(
          text: field.defaultValue ?? '',
        );
      }
    }
  }

  void _clearFormControllers() {
    for (final c in _textControllers.values) {
      c.dispose();
    }
    _textControllers.clear();
    _boolValues.clear();
    _selectValues.clear();
  }

  void _goBack() {
    _clearFormControllers();
    setState(() {
      _selectedSkill = null;
      _uiDescriptor = null;
      _executeResult = null;
      _uiError = null;
    });
  }

  void _closePanel() {
    context.go('/');
  }

  Future<void> _executeSkill() async {
    if (_selectedSkill == null || _uiDescriptor == null) return;

    // Validate required fields
    for (final field in _uiDescriptor!.formFields) {
      if (!field.required) continue;
      if (field.type == 'boolean') continue; // always has a value
      if (field.type == 'select') {
        if (_selectValues[field.name] == null ||
            _selectValues[field.name]!.isEmpty) {
          _showFieldError(field.label);
          return;
        }
      } else {
        final val = _textControllers[field.name]?.text.trim();
        if (val == null || val.isEmpty) {
          _showFieldError(field.label);
          return;
        }
      }
    }

    setState(() {
      _isExecuting = true;
      _executeResult = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final params = _collectFormValues();
      final result = await client.executeSkillWithParams(
        slug: _selectedSkill!.slug,
        params: params,
      );
      if (mounted) {
        setState(() {
          _executeResult = result;
          _isExecuting = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isExecuting = false;
          _executeResult = SkillExecuteResult(
            output: '',
            success: false,
            error: e.toString(),
          );
        });
      }
    }
  }

  Map<String, dynamic> _collectFormValues() {
    final params = <String, dynamic>{};
    for (final field in _uiDescriptor?.formFields ?? []) {
      if (field.type == 'boolean') {
        params[field.name] = _boolValues[field.name] ?? false;
      } else if (field.type == 'select') {
        params[field.name] = _selectValues[field.name] ?? '';
      } else {
        params[field.name] =
            _textControllers[field.name]?.text.trim() ?? '';
      }
    }
    return params;
  }

  void _showFieldError(String label) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          'required field: $label',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.orangePrimary,
            fontFamily: 'SourceCodePro',
          ),
        ),
        backgroundColor: CyberpunkColors.darkGray,
        duration: const Duration(seconds: 2),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return KeyboardListener(
      focusNode: FocusNode(),
      onKeyEvent: (KeyEvent event) {
        if (event.logicalKey == LogicalKeyboardKey.escape) {
          _closePanel();
        }
      },
      child: Container(
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border(
          top: BorderSide(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          _buildHeader(),
          Expanded(
            child: _isLoading
                ? const Center(
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
                : _error != null
                    ? _buildErrorState()
                    : _selectedSkill != null
                        ? _buildSkillDetail()
                        : _buildSkillList(),
          ),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          if (_selectedSkill != null) ...[
            GestureDetector(
              onTap: _goBack,
              child: const Icon(
                Icons.arrow_back,
                color: CyberpunkColors.orangePrimary,
                size: 18,
              ),
            ),
            const SizedBox(width: 8),
          ],
          const Icon(
            Icons.auto_awesome,
            color: CyberpunkColors.orangePrimary,
            size: 18,
          ),
          const SizedBox(width: 8),
          Text(
            _selectedSkill?.name ?? 'skills',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(Icons.close, size: 18),
            onPressed: _closePanel,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
            tooltip: 'close',
          ),
          if (_selectedSkill == null)
            GestureDetector(
              onTap: _loadSkills,
              child: const Icon(
                Icons.refresh,
                color: CyberpunkColors.orangePrimary,
                size: 16,
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildErrorState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.error_outline,
            color: CyberpunkColors.redAlert,
            size: 48,
          ),
          const SizedBox(height: 16),
          Text(
            'failed to load skills',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.redAlert,
            ),
          ),
          const SizedBox(height: 8),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 32),
            child: Text(
              _error ?? '',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangeDark,
              ),
              textAlign: TextAlign.center,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(height: 16),
          TextButton(
            onPressed: _loadSkills,
            child: Text(
              'retry',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSkillList() {
    if (_skills.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.auto_awesome_outlined,
              size: 48,
              color: CyberpunkColors.orangeDark,
            ),
            const SizedBox(height: 16),
            Text(
              'no skills available',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangeDark,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'skills from the daemon will appear here',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ],
        ),
      );
    }

    return ListView.separated(
      padding: const EdgeInsets.all(8),
      itemCount: _skills.length,
      separatorBuilder: (_, __) =>
          const SizedBox(height: 4),
      itemBuilder: (context, index) {
        final skill = _skills[index];
        return _buildSkillCard(skill);
      },
    );
  }

  Widget _buildSkillCard(Skill skill) {
    return GestureDetector(
      onTap: () => _selectSkill(skill.slug),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.black.withValues(alpha: 0.3),
          border: Border.all(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(
                  Icons.tune,
                  size: 16,
                  color: CyberpunkColors.orangeBright,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    skill.name.toLowerCase(),
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.orangePrimary,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            if (skill.description.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(
                skill.description,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                ),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ],
            if (skill.tags.isNotEmpty) ...[
              const SizedBox(height: 6),
              Wrap(
                spacing: 4,
                runSpacing: 4,
                children: skill.tags
                    .where((t) => t.isNotEmpty)
                    .take(5)
                    .map((t) => _buildChip(t))
                    .toList(),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildChip(String label) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        label.toLowerCase(),
        style: CyberpunkTypography.bodySmall.copyWith(
          fontSize: 9,
          color: CyberpunkColors.lightGray,
        ),
      ),
    );
  }

  Widget _buildSkillDetail() {
    if (_isLoadingUi) {
      return const Center(
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
      );
    }

    if (_uiError != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.error_outline,
              color: CyberpunkColors.redAlert,
              size: 48,
            ),
            const SizedBox(height: 16),
            Text(
              'failed to load skill ui',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 32),
              child: Text(
                _uiError ?? '',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangeDark,
                ),
                textAlign: TextAlign.center,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            const SizedBox(height: 16),
            TextButton(
              onPressed: () =>
                  _loadSkillUi(_selectedSkill!.slug),
              child: Text(
                'retry',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
            ),
          ],
        ),
      );
    }

    if (_uiDescriptor == null) {
      // No UI descriptor — show skill info with a simple execute prompt
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              _selectedSkill!.name.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              _selectedSkill!.description,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              textAlign: TextAlign.center,
            ),
          ],
        ),
      );
    }

    // Show form + execution result
    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        // Skill description
        if (_selectedSkill!.description.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(bottom: 16),
            child: Text(
              _selectedSkill!.description,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
                height: 1.4,
              ),
            ),
          ),

        // Dynamic form fields
        ..._uiDescriptor!.formFields.map(
          (field) => _buildFormField(field),
        ),

        const SizedBox(height: 16),

        // Execute button
        SizedBox(
          width: double.infinity,
          child: ElevatedButton(
            onPressed: _isExecuting ? null : _executeSkill,
            style: ElevatedButton.styleFrom(
              backgroundColor: CyberpunkColors.orangePrimary,
              foregroundColor: CyberpunkColors.black,
              padding: const EdgeInsets.symmetric(vertical: 12),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(2),
              ),
            ),
            child: _isExecuting
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      valueColor: AlwaysStoppedAnimation<Color>(
                        CyberpunkColors.black,
                      ),
                    ),
                  )
                : const Text(
                    'execute',
                    style: CyberpunkTypography.button,
                  ),
          ),
        ),

        // Execution result
        if (_executeResult != null) ...[
          const SizedBox(height: 16),
          _buildExecutionResult(),
        ],
      ],
    );
  }

  Widget _buildFormField(SkillFormField field) {
    final requiredSuffix =
        field.required ? ' *' : '';
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '${field.label.toLowerCase()}$requiredSuffix',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 4),
          switch (field.type) {
            'boolean' => _buildBooleanField(field),
            'select' => _buildSelectField(field),
            _ => _buildTextField(field),
          },
        ],
      ),
    );
  }

  Widget _buildTextField(SkillFormField field) {
    return TextField(
      controller: _textControllers[field.name],
      style: CyberpunkTypography.bodySmall.copyWith(
        fontFamily: 'SourceCodePro',
      ),
      decoration: InputDecoration(
        hintText: field.defaultValue ?? '',
        hintStyle: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.midGray,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(2),
          borderSide: const BorderSide(color: CyberpunkColors.midGray),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(2),
          borderSide: const BorderSide(color: CyberpunkColors.midGray),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(2),
          borderSide: const BorderSide(
            color: CyberpunkColors.orangePrimary,
            width: 1.5,
          ),
        ),
        filled: true,
        fillColor: CyberpunkColors.black,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 12,
          vertical: 8,
        ),
      ),
    );
  }

  Widget _buildBooleanField(SkillFormField field) {
    final value = _boolValues[field.name] ?? false;
    return GestureDetector(
      onTap: () {
        setState(() {
          _boolValues[field.name] = !value;
        });
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: CyberpunkColors.black,
          border: Border.all(color: CyberpunkColors.midGray),
          borderRadius: BorderRadius.circular(2),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              value ? Icons.check_box : Icons.check_box_outline_blank,
              size: 16,
              color: value
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.midGray,
            ),
            const SizedBox(width: 8),
            Text(
              value ? 'enabled' : 'disabled',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: value
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.midGray,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildSelectField(SkillFormField field) {
    final current = _selectValues[field.name];
    final options = field.options;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black,
        border: Border.all(color: CyberpunkColors.midGray),
        borderRadius: BorderRadius.circular(2),
      ),
      child: DropdownButtonHideUnderline(
        child: DropdownButton<String>(
          value: (current != null && options.contains(current))
              ? current
              : (options.isNotEmpty ? options.first : null),
          isExpanded: true,
          dropdownColor: CyberpunkColors.darkGray,
          style: CyberpunkTypography.bodySmall.copyWith(
            fontFamily: 'SourceCodePro',
          ),
          items: options.map((opt) {
            return DropdownMenuItem<String>(
              value: opt,
              child: Text(
                opt.toLowerCase(),
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                ),
              ),
            );
          }).toList(),
          onChanged: (val) {
            setState(() {
              _selectValues[field.name] = val;
            });
          },
        ),
      ),
    );
  }

  Widget _buildExecutionResult() {
    final result = _executeResult!;
    final success = result.success;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(
          color: success
              ? CyberpunkColors.greenSuccess.withValues(alpha: 0.5)
              : CyberpunkColors.redAlert.withValues(alpha: 0.5),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                success ? Icons.check_circle : Icons.error,
                size: 14,
                color: success
                    ? CyberpunkColors.greenSuccess
                    : CyberpunkColors.redAlert,
              ),
              const SizedBox(width: 6),
              Text(
                success ? 'completed' : 'failed',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: success
                      ? CyberpunkColors.greenSuccess
                      : CyberpunkColors.redAlert,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ],
          ),
          if (result.error != null) ...[
            const SizedBox(height: 8),
            SelectableText(
              result.error!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
          ],
          if (result.output.isNotEmpty) ...[
            const SizedBox(height: 8),
            SelectableText(
              result.output,
              style: CyberpunkTypography.bodySmall.copyWith(
                fontFamily: 'SourceCodePro',
                color: CyberpunkColors.lightGray,
                height: 1.4,
              ),
              maxLines: 20,
            ),
          ],
        ],
      ),
    );
  }
}
