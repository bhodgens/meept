import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../services/api_client.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';

/// Settings panel - edit configuration files
class SettingsPanel extends ConsumerStatefulWidget {
  const SettingsPanel({super.key});

  @override
  ConsumerState<SettingsPanel> createState() => _SettingsPanelState();
}

class _SettingsPanelState extends ConsumerState<SettingsPanel> {
  String _selectedConfig = 'client';
  String _configContent = '';
  bool _isLoading = true;
  bool _isSaving = false;
  String? _error;
  bool _hasChanges = false;

  late final ApiClient _client;
  late final TextEditingController _controller;

  final Map<String, String> _configLabels = {
    'client': 'client.json5',
    'models': 'models.json5',
    'menubar': 'menubar.json5',
  };

  @override
  void initState() {
    super.initState();
    _client = ref.read(apiClientProvider);
    _controller = TextEditingController(text: _configContent);
    _loadConfig();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
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
          break;
        case 'models':
          content = await _client.getModelsConfig();
          break;
        case 'menubar':
          content = await _client.getMenubarConfig();
          break;
        default:
          content = '';
      }
      if (mounted) {
        setState(() {
          _configContent = content;
          _controller.text = content;
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

  Future<void> _saveConfig() async {
    setState(() {
      _isSaving = true;
    });
    try {
      switch (_selectedConfig) {
        case 'client':
          await _client.saveClientConfig(_configContent);
          break;
        case 'models':
          await _client.saveModelsConfig(_configContent);
          break;
        case 'menubar':
          await _client.saveMenubarConfig(_configContent);
          break;
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
      SnackBar(
        content: const Text('config saved successfully'),
        backgroundColor: CyberpunkColors.greenSuccess,
        duration: const Duration(seconds: 2),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          _buildHeader(),
          if (_error != null) _buildErrorBanner(),
          Expanded(child: _buildEditor()),
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
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
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
                          setState(() {
                            _selectedConfig = entry.key;
                          });
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
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }

  void _showDiscardDialog(String newConfig) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'discard changes?',
          style: CyberpunkTypography.bodyMedium,
        ),
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
              style: const TextStyle(
                color: Color(0xFFFF6060),
                fontSize: 10,
              ),
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
          Expanded(
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
                    if (!_hasChanges) {
                      _hasChanges = true;
                    }
                  });
                },
                controller: _controller,
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
