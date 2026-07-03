import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/storage_service.dart';

/// Modifier key preference: "ctrl" or "cmd".
/// Defaults to "ctrl" on all platforms.
final modifierKeyProvider = StateNotifierProvider<ModifierKeyNotifier, String>((ref) {
  return ModifierKeyNotifier();
});

class ModifierKeyNotifier extends StateNotifier<String> {
  ModifierKeyNotifier() : super('ctrl');

  /// Load the preference from storage. Call this at app startup.
  Future<void> load() async {
    final value = StorageService.instance.getModifierKey();
    state = value;
  }

  /// Set the preference and persist to storage.
  Future<void> set(String value) async {
    await StorageService.instance.setModifierKey(value);
    state = value;
  }

  /// Toggle between "ctrl" and "cmd".
  Future<void> toggle() async {
    final newValue = state == 'ctrl' ? 'cmd' : 'ctrl';
    await set(newValue);
  }
}
