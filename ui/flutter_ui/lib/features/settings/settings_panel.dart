import 'package:flutter/material.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../services/api_client.dart';
import '../../services/storage_service.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'settings_inputs.dart';

/// Form field names used throughout the settings panel.
class SettingsFields {
  static const String daemonHost = 'daemon_host';
  static const String daemonPort = 'daemon_port';
  static const String theme = 'theme';
  static const String apiKey = 'api_key';
  static const String sttEnabled = 'stt_enabled';
  static const String sttEngine = 'stt_engine';
  static const String sttLanguage = 'stt_language';
  static const String sttAutoSend = 'stt_auto_send';
}

/// Settings panel - edit configuration files and connection settings.
///
/// Uses [FormBuilder] with [formz] validators for structured form state.
/// The panel is divided into four sections:
///   1. Connection (daemon host, port, theme)
///   2. API token (keychain-backed)
///   3. Speech-to-text (engine, language, auto-send)
///   4. Config file editor (client/models/menubar JSON5)
class SettingsPanel extends ConsumerStatefulWidget {
  const SettingsPanel({super.key});

  @override
  ConsumerState<SettingsPanel> createState() => _SettingsPanelState();
}

class _SettingsPanelState extends ConsumerState<SettingsPanel> {
  final _formKey = GlobalKey<FormBuilderState>();

  String _selectedConfig = 'client';
  String _configContent = '';
  bool _isLoading = true;
  bool _isSaving = false;
  bool _isSavingConnection = false;
  String? _error;
  bool _hasChanges = false;

  bool _apiKeyObscured = true;
  String? _apiKeyStatus;

  late final ApiClient _client;
  late final TextEditingController _configController;

  final Map<String, String> _configLabels = {
    'client': 'client.json5',
    'models': 'models.json5',
    'menubar': 'menubar.json5',
  };

  @override
  void initState() {
    super.initState();
    _client = ref.read(apiClientProvider);
    _configController = TextEditingController(text: _configContent);
    _loadApiKey();
    _loadConfig();
  }

  @override
  void dispose() {
    _configController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  Future<void> _loadApiKey() async {
    final apiKey = await StorageService.instance.getApiKeyAsync();
    if (mounted) {
      setState(() {
        _apiKeyStatus = apiKey != null && apiKey.isNotEmpty
            ? 'token configured'
            : 'no token configured';
      });
      // Pre-fill the form field once the form is built.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _formKey.currentState?.fields != null) {
          _formKey.currentState?.patchValue({
            SettingsFields.apiKey: apiKey ?? '',
          });
        }
      });
    }
  }

  Future<void> _loadConfig() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      String content;
      switch (_selectedConfig) {
        case 'client':
          content = await _client.getClientConfig();
        case 'models':
          content = await _client.getModelsConfig();
        case 'menubar':
          content = await _client.getMenubarConfig();
        default:
          content = '';
      }
      if (mounted) {
        setState(() {
          _configContent = content;
          _configController.text = content;
          _isLoading = false;
          _hasChanges = false;
        });
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

  // ---------------------------------------------------------------------------
  // Persistence
  // ---------------------------------------------------------------------------

  Future<void> _saveConnectionSettings() async {
    if (!_formKey.currentState!.saveAndValidate()) return;

    setState(() => _isSavingConnection = true);
    try {
      final storage = StorageService.instance;
      final host = _formKey.currentState!.value[SettingsFields.daemonHost] as String?;
      final port = _formKey.currentState!.value[SettingsFields.daemonPort] as int?;
      final theme = _formKey.currentState!.value[SettingsFields.theme] as String?;

      if (host != null && host.isNotEmpty) await storage.setApiHost(host);
      if (port != null) await storage.setApiPort(port);
      if (theme != null) await storage.setTheme(theme);

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('connection settings saved'),
            backgroundColor: CyberpunkColors.greenSuccess,
            duration: Duration(seconds: 2),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('failed to save: $e'),
            backgroundColor: CyberpunkColors.redAlert,
            duration: const Duration(seconds: 3),
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _isSavingConnection = false);
    }
  }

  Future<void> _saveApiKey() async {
    final apiKey = (_formKey.currentState?.value[SettingsFields.apiKey] as String?) ?? '';
    await StorageService.instance.setApiKey(apiKey.trim());
    if (mounted) {
      setState(() {
        _apiKeyStatus = apiKey.trim().isNotEmpty ? 'token configured' : 'no token configured';
      });
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('API token saved to keychain'),
          backgroundColor: CyberpunkColors.greenSuccess,
          duration: Duration(seconds: 2),
        ),
      );
    }
  }

  Future<void> _saveConfig() async {
    setState(() => _isSaving = true);
    try {
      switch (_selectedConfig) {
        case 'client':
          await _client.saveClientConfig(_configContent);
        case 'models':
          await _client.saveModelsConfig(_configContent);
        case 'menubar':
          await _client.saveMenubarConfig(_configContent);
      }
      if (mounted) {
        setState(() {
          _isSaving = false;
          _hasChanges = false;
          _error = null;
        });
        _showSaveSuccess();
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isSaving = false;
          _error = e.toString();
        });
      }
    }
  }

  void _showSaveSuccess() {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('config saved successfully'),
        backgroundColor: CyberpunkColors.greenSuccess,
        duration: Duration(seconds: 2),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    final storage = StorageService.instance;
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          _buildHeader(),
          Expanded(
            child: ListView(
              padding: EdgeInsets.zero,
              children: [
                _buildConnectionSection(storage),
                if (_error != null) _buildErrorBanner(),
                _buildEditor(),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              GestureDetector(
                onTap: () => context.go('/'),
                child: const Icon(
                  Icons.arrow_back,
                  color: CyberpunkColors.orangePrimary,
                  size: 18,
                ),
              ),
              const SizedBox(width: 8),
              const Icon(
                Icons.settings,
                color: CyberpunkColors.orangePrimary,
                size: 18,
              ),
              const SizedBox(width: 8),
              Text(
                'settings',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const Spacer(),
              if (_isSaving)
                const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      CyberpunkColors.orangePrimary,
                    ),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              ..._configLabels.entries.map((entry) {
                final isSelected = _selectedConfig == entry.key;
                return Padding(
                  padding: const EdgeInsets.only(right: 4),
                  child: ChoiceChip(
                    label: Text(
                      entry.value.toLowerCase(),
                      style: CyberpunkTypography.bodySmall.copyWith(
                        fontFamily: 'SourceCodePro',
                        fontSize: 10,
                      ),
                    ),
                    selected: isSelected,
                    selectedColor: CyberpunkColors.orangeDark,
                    backgroundColor: CyberpunkColors.midGray.withValues(alpha: 0.2),
                    labelStyle: TextStyle(
                      color: isSelected
                          ? CyberpunkColors.orangeBright
                          : CyberpunkColors.lightGray,
                    ),
                    onSelected: (selected) {
                      if (selected && _selectedConfig != entry.key) {
                        if (_hasChanges) {
                          _showDiscardDialog(entry.key);
                        } else {
                          setState(() => _selectedConfig = entry.key);
                          _loadConfig();
                        }
                      }
                    },
                  ),
                );
              }),
              const Spacer(),
              if (_hasChanges)
                ElevatedButton.icon(
                  onPressed: _saveConfig,
                  icon: const Icon(Icons.save, size: 16),
                  label: const Text('save'),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: CyberpunkColors.greenSuccess,
                    foregroundColor: CyberpunkColors.black,
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // FormBuilder sections
  // ---------------------------------------------------------------------------

  Widget _buildConnectionSection(StorageService storage) {
    return FormBuilder(
      key: _formKey,
      initialValue: {
        SettingsFields.daemonHost: storage.getApiHost() ?? 'localhost',
        SettingsFields.daemonPort: storage.getApiPort() ?? 8081,
        SettingsFields.theme: storage.getTheme() ?? 'cyberpunk',
        SettingsFields.apiKey: '',
        SettingsFields.sttEnabled: false,
        SettingsFields.sttEngine: 'native',
        SettingsFields.sttLanguage: 'en',
        SettingsFields.sttAutoSend: false,
      },
      child: _FormSections(
        apiKeyObscured: _apiKeyObscured,
        apiKeyStatus: _apiKeyStatus,
        onToggleApiKeyObscure: () {
          setState(() => _apiKeyObscured = !_apiKeyObscured);
        },
        onSaveApiKey: _saveApiKey,
        onSaveConnection: _saveConnectionSettings,
        isSavingConnection: _isSavingConnection,
      ),
    );
  }

  void _showDiscardDialog(String newConfig) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text('discard changes?', style: CyberpunkTypography.bodyMedium),
        content: Text(
          'you have unsaved changes in ${_configLabels[_selectedConfig]}. discard?',
          style: CyberpunkTypography.bodySmall,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('cancel'),
          ),
          TextButton(
            onPressed: () {
              Navigator.pop(context);
              setState(() {
                _selectedConfig = newConfig;
                _hasChanges = false;
              });
              _loadConfig();
            },
            child: const Text('discard'),
          ),
        ],
      ),
    );
  }

  Widget _buildErrorBanner() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      color: const Color(0x80FF3030),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: Color(0xFFFF6060), size: 14),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _error!,
              style: const TextStyle(color: Color(0xFFFF6060), fontSize: 10),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          GestureDetector(
            onTap: _loadConfig,
            child: const Icon(Icons.refresh, color: Color(0xFFFF6060), size: 14),
          ),
        ],
      ),
    );
  }

  Widget _buildEditor() {
    if (_isLoading) {
      return const Center(
        child: SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(
            strokeWidth: 2,
            valueColor: AlwaysStoppedAnimation<Color>(
              CyberpunkColors.orangePrimary,
            ),
          ),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.all(12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '// ${_configLabels[_selectedConfig]}',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
          const SizedBox(height: 8),
          SizedBox(
            height: 300,
            child: Container(
              decoration: BoxDecoration(
                color: CyberpunkColors.black,
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: CyberpunkColors.midGray),
              ),
              child: TextField(
                style: CyberpunkTypography.bodySmall.copyWith(
                  fontFamily: 'SourceCodePro',
                  height: 1.4,
                ),
                maxLines: null,
                expands: true,
                textAlignVertical: TextAlignVertical.top,
                onChanged: (value) {
                  setState(() {
                    _configContent = value;
                    if (!_hasChanges) _hasChanges = true;
                  });
                },
                controller: _configController,
                decoration: const InputDecoration(
                  hintText: '// edit configuration...',
                  hintStyle: TextStyle(
                    color: CyberpunkColors.midGray,
                    fontFamily: 'SourceCodePro',
                  ),
                  border: InputBorder.none,
                  contentPadding: EdgeInsets.all(12),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

// =============================================================================
// _FormSections — the actual FormBuilder children extracted into a widget
// so that ConsumerState can still manage the non-form state (apiKeyObscured,
// apiKeyStatus, etc.).
// =============================================================================

class _FormSections extends StatelessWidget {
  final bool apiKeyObscured;
  final String? apiKeyStatus;
  final VoidCallback onToggleApiKeyObscure;
  final VoidCallback onSaveApiKey;
  final VoidCallback onSaveConnection;
  final bool isSavingConnection;

  const _FormSections({
    required this.apiKeyObscured,
    required this.apiKeyStatus,
    required this.onToggleApiKeyObscure,
    required this.onSaveApiKey,
    required this.onSaveConnection,
    required this.isSavingConnection,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _buildConnectionSection(context),
        _buildApiKeySection(context),
        _buildSttSection(context),
      ],
    );
  }

  // ---------------------------------------------------------------------------
  // Section: connection (daemon host, port, theme)
  // ---------------------------------------------------------------------------

  Widget _buildConnectionSection(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Section header
          Row(
            children: [
              const Icon(Icons.cloud, color: CyberpunkColors.blueInfo, size: 16),
              const SizedBox(width: 8),
              Text(
                'connection',
                style: CyberpunkTypography.label.copyWith(color: CyberpunkColors.blueInfo),
              ),
              const Spacer(),
              ElevatedButton(
                onPressed: isSavingConnection ? null : onSaveConnection,
                style: ElevatedButton.styleFrom(
                  backgroundColor: CyberpunkColors.orangePrimary,
                  foregroundColor: CyberpunkColors.black,
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                ),
                child: isSavingConnection
                    ? const SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          valueColor: AlwaysStoppedAnimation<Color>(CyberpunkColors.black),
                        ),
                      )
                    : const Text('save connection'),
              ),
            ],
          ),
          const SizedBox(height: 12),
          // Daemon host
          FormBuilderTextField(
            name: SettingsFields.daemonHost,
            decoration: InputDecoration(
              labelText: 'daemon host',
              labelStyle: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              hintText: 'localhost or 192.168.1.10',
              prefixIcon: const Icon(Icons.dns, color: CyberpunkColors.orangePrimary, size: 18),
              isDense: true,
              filled: true,
              fillColor: CyberpunkColors.black,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              enabledBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              focusedBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.orangePrimary, width: 1.5),
              ),
              errorStyle: const TextStyle(color: CyberpunkColors.redAlert, fontSize: 10),
            ),
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
            ),
            validator: (value) => DaemonHostInput.dirty(value ?? '').error,
          ),
          const SizedBox(height: 10),
          // Daemon port
          FormBuilderTextField(
            name: SettingsFields.daemonPort,
            decoration: InputDecoration(
              labelText: 'daemon port',
              labelStyle: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              hintText: '8081',
              prefixIcon: const Icon(Icons.numbers, color: CyberpunkColors.orangePrimary, size: 18),
              isDense: true,
              filled: true,
              fillColor: CyberpunkColors.black,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              enabledBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              focusedBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.orangePrimary, width: 1.5),
              ),
              errorStyle: const TextStyle(color: CyberpunkColors.redAlert, fontSize: 10),
            ),
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
            ),
            keyboardType: TextInputType.number,
            validator: (value) {
              final parsed = int.tryParse(value ?? '');
              if (parsed == null) return 'port must be a number';
              return DaemonPortInput.dirty(parsed).error;
            },
            valueTransformer: (value) => int.tryParse(value ?? '') ?? 8081,
          ),
          const SizedBox(height: 10),
          // Theme selection
          FormBuilderDropdown<String>(
            name: SettingsFields.theme,
            decoration: InputDecoration(
              labelText: 'theme',
              labelStyle: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              prefixIcon: const Icon(Icons.palette, color: CyberpunkColors.orangePrimary, size: 18),
              isDense: true,
              filled: true,
              fillColor: CyberpunkColors.black,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              enabledBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              focusedBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.orangePrimary, width: 1.5),
              ),
            ),
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
              color: CyberpunkColors.lightGray,
            ),
            dropdownColor: CyberpunkColors.darkGray,
            items: ['cyberpunk', 'midnight', 'solarized']
                .map((theme) => DropdownMenuItem(
                      value: theme,
                      child: Text(
                        theme.toLowerCase(),
                        style: CyberpunkTypography.bodySmall.copyWith(
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                    ))
                .toList(),
          ),
        ],
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Section: API token
  // ---------------------------------------------------------------------------

  Widget _buildApiKeySection(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Section header
          Row(
            children: [
              const Icon(Icons.lock_outline, color: CyberpunkColors.greenSuccess, size: 16),
              const SizedBox(width: 8),
              Text(
                'API token (stored in macos keychain)',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.greenSuccess,
                ),
              ),
              const Spacer(),
              if (apiKeyStatus != null)
                Text(
                  apiKeyStatus!,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                    fontSize: 9,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 8),
          FormBuilderTextField(
            name: SettingsFields.apiKey,
            obscureText: apiKeyObscured,
            decoration: InputDecoration(
              labelText: 'API token',
              labelStyle: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              hintText: 'enter API token...',
              prefixIcon: const Icon(Icons.key, color: CyberpunkColors.greenSuccess, size: 18),
              suffixIcon: IconButton(
                icon: Icon(
                  apiKeyObscured ? Icons.visibility : Icons.visibility_off,
                  color: CyberpunkColors.orangePrimary,
                  size: 18,
                ),
                onPressed: onToggleApiKeyObscure,
                tooltip: apiKeyObscured ? 'show token' : 'hide token',
              ),
              isDense: true,
              filled: true,
              fillColor: CyberpunkColors.black,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              enabledBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              focusedBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(6),
                borderSide: const BorderSide(color: CyberpunkColors.greenSuccess, width: 1.5),
              ),
              errorStyle: const TextStyle(color: CyberpunkColors.redAlert, fontSize: 10),
            ),
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
            ),
            validator: (value) => ApiTokenInput.dirty(value ?? '').error,
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              ElevatedButton(
                onPressed: onSaveApiKey,
                style: ElevatedButton.styleFrom(
                  backgroundColor: CyberpunkColors.orangePrimary,
                  foregroundColor: CyberpunkColors.black,
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                ),
                child: const Text('save token'),
              ),
              const SizedBox(width: 12),
              Text(
                'run `meept token generate --save` in terminal to create a token',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                  fontSize: 9,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Section: speech-to-text
  // ---------------------------------------------------------------------------

  Widget _buildSttSection(BuildContext context) {
    return FormBuilderField<bool>(
      name: SettingsFields.sttEnabled,
      builder: (FormFieldState<bool?> field) {
        final enabled = field.value ?? false;
        return InputDecorator(
          decoration: const InputDecoration(
            border: InputBorder.none,
            isDense: true,
            contentPadding: EdgeInsets.zero,
          ),
          child: Container(
            padding: const EdgeInsets.all(12),
            decoration: const BoxDecoration(
              border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // Section header with switch
                Row(
                  children: [
                    const Icon(Icons.mic, color: CyberpunkColors.orangePrimary, size: 16),
                    const SizedBox(width: 8),
                    Text(
                      'speech-to-text',
                      style: CyberpunkTypography.label.copyWith(
                        color: CyberpunkColors.orangePrimary,
                      ),
                    ),
                    const Spacer(),
                    FormBuilderSwitch(
                      name: SettingsFields.sttEnabled,
                      title: const SizedBox.shrink(),
                      activeColor: CyberpunkColors.orangePrimary,
                      inactiveTrackColor: CyberpunkColors.midGray,
                      controlAffinity: ListTileControlAffinity.leading,
                      decoration: const InputDecoration(border: InputBorder.none),
                      onChanged: (value) {
                        // Rebuild to show/hide sub-fields.
                        // FormBuilder handles state internally.
                      },
                    ),
                  ],
                ),
                if (enabled) ...[
                  const SizedBox(height: 8),
                  // Engine selector
                  FormBuilderDropdown<String>(
                    name: SettingsFields.sttEngine,
                    decoration: InputDecoration(
                      labelText: 'engine',
                      labelStyle: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.lightGray,
                      ),
                      isDense: true,
                      filled: true,
                      fillColor: CyberpunkColors.black,
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(6),
                        borderSide: const BorderSide(color: CyberpunkColors.midGray),
                      ),
                      enabledBorder: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(6),
                        borderSide: const BorderSide(color: CyberpunkColors.midGray),
                      ),
                      focusedBorder: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(6),
                        borderSide: const BorderSide(color: CyberpunkColors.orangePrimary, width: 1.5),
                      ),
                    ),
                    style: CyberpunkTypography.bodySmall.copyWith(
                      fontFamily: 'SourceCodePro',
                      color: CyberpunkColors.lightGray,
                    ),
                    dropdownColor: CyberpunkColors.darkGray,
                    items: ['native', 'whisper', 'parakeet']
                        .map((engine) => DropdownMenuItem(
                              value: engine,
                              child: Text(
                                engine.toLowerCase(),
                                style: CyberpunkTypography.bodySmall.copyWith(
                                  fontFamily: 'SourceCodePro',
                                ),
                              ),
                            ))
                        .toList(),
                  ),
                  const SizedBox(height: 10),
                  // Language + auto-send row
                  Row(
                    children: [
                      Expanded(
                        child: FormBuilderTextField(
                          name: SettingsFields.sttLanguage,
                          decoration: InputDecoration(
                            labelText: 'language',
                            labelStyle: CyberpunkTypography.bodySmall.copyWith(
                              color: CyberpunkColors.lightGray,
                            ),
                            hintText: 'en',
                            isDense: true,
                            filled: true,
                            fillColor: CyberpunkColors.black,
                            border: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(6),
                              borderSide: const BorderSide(color: CyberpunkColors.midGray),
                            ),
                            enabledBorder: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(6),
                              borderSide: const BorderSide(color: CyberpunkColors.midGray),
                            ),
                            focusedBorder: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(6),
                              borderSide: const BorderSide(color: CyberpunkColors.orangePrimary, width: 1.5),
                            ),
                            errorStyle: const TextStyle(color: CyberpunkColors.redAlert, fontSize: 10),
                          ),
                          style: CyberpunkTypography.bodySmall.copyWith(
                            fontFamily: 'SourceCodePro',
                          ),
                          validator: (value) => SttLanguageInput.dirty(value ?? 'en').error,
                        ),
                      ),
                      const SizedBox(width: 16),
                      // Auto-send toggle
                      FormBuilderSwitch(
                        name: SettingsFields.sttAutoSend,
                        title: Text(
                          'auto-send',
                          style: CyberpunkTypography.bodySmall.copyWith(
                            color: CyberpunkColors.lightGray,
                          ),
                        ),
                        activeColor: CyberpunkColors.orangePrimary,
                        inactiveTrackColor: CyberpunkColors.midGray,
                        controlAffinity: ListTileControlAffinity.leading,
                        decoration: const InputDecoration(border: InputBorder.none),
                      ),
                    ],
                  ),
                ],
              ],
            ),
          ),
        );
      },
    );
  }
}
