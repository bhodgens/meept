import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../core/constants.dart';
import '../core/platform/platform_service.dart' show platformService, initPlatformService;

/// Centralized persistent storage backed by [SharedPreferences] and
/// macOS Keychain (via [FlutterSecureStorage]) for sensitive data.
///
/// The service is a singleton that must be initialized via [init] in
/// [main] before any synchronous reads are performed. Once [init] has
/// completed, all subsequent getter calls are synchronous.
class StorageService {
  static final StorageService _instance = StorageService._();

  StorageService._();

  /// Global singleton accessor. Call [init] once at app startup.
  static StorageService get instance => _instance;

  SharedPreferences? _prefs;
  FlutterSecureStorage? _secureStorage;
  String? _cachedApiKey;

  /// Whether [init] has been called and [_prefs] is populated.
  bool get isInitialized => _prefs != null;

  /// Initialize the underlying storage instances.
  /// Must be called (awaits) before any synchronous reads.
  ///
  /// Resilient to storage plugin failures: if SharedPreferences or
  /// FlutterSecureStorage fail to initialize, the app continues with
  /// null storage (all getters return null). This prevents crashes
  /// on misconfigured platforms while allowing the UI to render.
  Future<void> init() async {
    // Ensure platform service is initialized first
    await initPlatformService();

    try {
      _prefs ??= await SharedPreferences.getInstance();
    } catch (e) {
      debugPrint('[warn] SharedPreferences init failed: $e');
      // Continue with null _prefs - getters will return null
    }

    try {
      // Configure for macOS keychain
      _secureStorage ??= const FlutterSecureStorage(
          aOptions: AndroidOptions(
            encryptedSharedPreferences: true,
          ),
          iOptions: IOSOptions(
            accessibility: KeychainAccessibility.first_unlock_this_device,
          ),
          mOptions: MacOsOptions(
            accessibility: KeychainAccessibility.first_unlock_this_device,
          ),
        );

      // Cache API key from keychain so synchronous reads use secure storage
      _cachedApiKey = await _secureStorage?.read(key: AppConstants.apiKeyPref);
    } catch (e) {
      debugPrint('[warn] FlutterSecureStorage init failed: $e');
      // Continue with null _secureStorage - falls back to SharedPreferences
    }

    // If no key was found in keychain/prefs, try reading the daemon's
    // per-installation dev key from ~/.meept/dev_key. The platform service
    // gracefully returns null on web, and on native platforms returns the
    // correct home directory.
    if (_cachedApiKey == null || _cachedApiKey!.isEmpty) {
      _cachedApiKey = await _tryReadDevKeyFile();
    }

    // Load GUI layout from client.json5 file for file-based config support
    await loadGuiLayoutFromFile();
  }

  /// Attempt to read the daemon's per-installation dev key from
  /// `~/.meept/dev_key`. Returns null if the file doesn't exist or
  /// can't be read.
  ///
  /// On web, [platformService] returns null for the home directory,
  /// so this method returns null immediately without filesystem access.
  Future<String?> _tryReadDevKeyFile() async {
    final home = await platformService?.getHomeDirectory();
    if (home == null) return null;

    final path = '$home/.meept/dev_key';
    final key = await platformService?.readDevKeyFile(path);
    if (key != null && key.isNotEmpty) {
      debugPrint('[storage] Loaded dev key from $path');
      return key;
    }
    return null;
  }

  // ------ API Key (secure storage) ------

  /// Read API key synchronously.
  String? getApiKey() {
    if (_cachedApiKey != null && _cachedApiKey!.isNotEmpty) {
      return _cachedApiKey;
    }
    final prefsKey = _prefs?.getString(AppConstants.apiKeyPref);
    if (prefsKey != null && prefsKey.isNotEmpty) return prefsKey;
    if (AppConstants.defaultApiKey.isNotEmpty) return AppConstants.defaultApiKey;
    return null;
  }

  /// Read API key from keychain (async) for full security.
  Future<String?> getApiKeyAsync() async {
    final keychainKey = await _secureStorage?.read(key: AppConstants.apiKeyPref);
    if (keychainKey != null) return keychainKey;
    final prefsKey = _prefs?.getString(AppConstants.apiKeyPref);
    if (prefsKey != null) return prefsKey;
    if (AppConstants.defaultApiKey.isNotEmpty) return AppConstants.defaultApiKey;
    return null;
  }

  /// Write API key to both keychain and SharedPreferences.
  Future<void> setApiKey(String key) async {
    _cachedApiKey = key;
    await _secureStorage?.write(key: AppConstants.apiKeyPref, value: key);
    await _prefs?.setString(AppConstants.apiKeyPref, key);
  }

  /// Remove API key from both storage backends.
  Future<void> clearApiKey() async {
    _cachedApiKey = null;
    await _secureStorage?.delete(key: AppConstants.apiKeyPref);
    await _prefs?.remove(AppConstants.apiKeyPref);
  }

  // ------ Theme ------

  String? getTheme() => _prefs?.getString(AppConstants.themePref);

  Future<void> setTheme(String theme) async {
    await _prefs?.setString(AppConstants.themePref, theme);
  }

  // ------ TTS Settings ------

  bool getTtsEnabled() => _prefs?.getBool(AppConstants.ttsEnabledPref) ?? false;

  Future<void> setTtsEnabled(bool enabled) async {
    await _prefs?.setBool(AppConstants.ttsEnabledPref, enabled);
  }

  String? getTtsVoice() => _prefs?.getString(AppConstants.ttsVoicePref);

  Future<void> setTtsVoice(String voice) async {
    await _prefs?.setString(AppConstants.ttsVoicePref, voice);
  }

  double getTtsVolume() => _prefs?.getDouble(AppConstants.ttsVolumePref) ?? 1.0;

  Future<void> setTtsVolume(double volume) async {
    await _prefs?.setDouble(AppConstants.ttsVolumePref, volume);
  }

  double getTtsRate() => _prefs?.getDouble(AppConstants.ttsRatePref) ?? 0.5;

  Future<void> setTtsRate(double rate) async {
    await _prefs?.setDouble(AppConstants.ttsRatePref, rate);
  }

  bool getTtsInterrupt() => _prefs?.getBool(AppConstants.ttsInterruptPref) ?? true;

  Future<void> setTtsInterrupt(bool interrupt) async {
    await _prefs?.setBool(AppConstants.ttsInterruptPref, interrupt);
  }

  bool getTtsQueue() => _prefs?.getBool(AppConstants.ttsQueuePref) ?? false;

  Future<void> setTtsQueue(bool queue) async {
    await _prefs?.setBool(AppConstants.ttsQueuePref, queue);
  }

  int getTtsMaxQueueSize() => _prefs?.getInt(AppConstants.ttsMaxQueueSizePref) ?? 5;

  Future<void> setTtsMaxQueueSize(int size) async {
    await _prefs?.setInt(AppConstants.ttsMaxQueueSizePref, size);
  }

  // ------ Connection / Host ------

  String? getApiHost() => _prefs?.getString(_hostPref);

  Future<void> setApiHost(String host) async {
    await _prefs?.setString(_hostPref, host);
  }

  int? getApiPort() => _prefs?.getInt(_portPref);

  Future<void> setApiPort(int port) async {
    await _prefs?.setInt(_portPref, port);
  }

  // ------ Keybindings ------

  /// Leader key preference: "cmd+x" (macOS) or "ctrl+x" (linux/win).
  String getLeaderKey() {
    final stored = _prefs?.getString(_leaderKeyPref);
    if (stored != null) return stored;
    return platformService?.defaultLeaderKey ?? 'ctrl+x';
  }

  Future<void> setLeaderKey(String value) async {
    await _prefs?.setString(_leaderKeyPref, value);
  }

  /// Modifier key preference: "ctrl" or "cmd".
  String getModifierKey() {
    return _prefs?.getString(_modifierKeyPref) ?? 'ctrl';
  }

  Future<void> setModifierKey(String value) async {
    await _prefs?.setString(_modifierKeyPref, value);
  }

  /// GUI layout preference: "toptabs" (default) or "sidebar".
  /// Reads from SharedPreferences first, then falls back to client.json5 file,
  /// then to the MEEPT_GUI_LAYOUT dart-define (injected by Makefile for web).
  String? getGuiLayout() {
    // SharedPreferences takes priority (set via UI)
    final prefsValue = _prefs?.getString(_guiLayoutPref);
    if (prefsValue != null) return prefsValue;

    // Fall back to cached client.json5 value
    if (_cachedClientGuiLayout != null) return _cachedClientGuiLayout;

    // Fall back to build-time dart-define (web can't read files)
    const defineValue = String.fromEnvironment('MEEPT_GUI_LAYOUT');
    if (defineValue.isNotEmpty) return defineValue;

    return null;
  }

  Future<void> setGuiLayout(String value) async {
    await _prefs?.setString(_guiLayoutPref, value);
  }

  /// Cache for GUI layout from client.json5 file
  String? _cachedClientGuiLayout;

  /// Load GUI layout from client.json5 file (~/.meept/client.json5).
  /// Call this at app startup before getGuiLayout() is used.
  Future<void> loadGuiLayoutFromFile() async {
    if (kIsWeb) return; // Web can't read files

    final home = await platformService?.getHomeDirectory();
    if (home == null) return;

    final path = '$home/.meept/client.json5';
    final content = await platformService?.readFile(path);
    if (content == null || content.isEmpty) return;

    // Simple JSON5 parsing for gui.layout field
    // Look for "gui": { ..."layout": "value" } pattern
    final layoutRegex = RegExp(r'"gui"\s*:\s*\{[^}]*"layout"\s*:\s*"([^"]+)"');
    final match = layoutRegex.firstMatch(content);
    if (match != null && match.groupCount >= 1) {
      _cachedClientGuiLayout = match.group(1);
      debugPrint('[storage] Loaded gui.layout from $path: $_cachedClientGuiLayout');
    }
  }

  /// Double-enter behavior: "steer", "interrupt", or "preempt".
  String getDoubleEnter() {
    return _prefs?.getString(_doubleEnterPref) ?? 'steer';
  }

  Future<void> setDoubleEnter(String value) async {
    await _prefs?.setString(_doubleEnterPref, value);
  }

  // ------ General helpers ------

  bool? getBool(String key) => _prefs?.getBool(key);

  Future<void> setBool(String key, bool value) async {
    await _prefs?.setBool(key, value);
  }

  double? getDouble(String key) => _prefs?.getDouble(key);

  Future<void> setDouble(String key, double value) async {
    await _prefs?.setDouble(key, value);
  }

  Future<bool> clearAll() async {
    await _secureStorage?.deleteAll();
    return await _prefs?.clear() ?? false;
  }

  bool containsKey(String key) => _prefs?.containsKey(key) ?? false;

  // ------ Internal keys ------

  static const String _hostPref = 'api_host';
  static const String _portPref = 'api_port';
  static const String _leaderKeyPref = 'leader_key';
  static const String _modifierKeyPref = 'modifier_key';
  static const String _guiLayoutPref = 'gui_layout';
  static const String _doubleEnterPref = 'double_enter';
}
