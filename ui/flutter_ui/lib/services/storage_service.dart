import 'dart:io';

import 'package:shared_preferences/shared_preferences.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../core/constants.dart';

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
  Future<void> init() async {
    _prefs ??= await SharedPreferences.getInstance();
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
  }

  // ------ API Key (secure storage) ------

  /// Read API key synchronously.
  /// Returns the cached keychain value if [init] has been awaited,
  /// otherwise falls back to SharedPreferences for backward compatibility.
  /// Returns null if no API key is configured - caller must handle this case.
  String? getApiKey() {
    if (_cachedApiKey != null) return _cachedApiKey;
    return _prefs?.getString(AppConstants.apiKeyPref);
  }

  /// Read API key from keychain (async) for full security.
  /// Falls back to SharedPreferences if keychain unavailable.
  Future<String?> getApiKeyAsync() async {
    // Try keychain first
    final keychainKey = await _secureStorage?.read(key: AppConstants.apiKeyPref);
    if (keychainKey != null) return keychainKey;
    // Fallback to SharedPreferences for backward compatibility
    return _prefs?.getString(AppConstants.apiKeyPref);
  }

  /// Write API key to both keychain and SharedPreferences.
  /// Keychain is primary; SharedPreferences is for backward compatibility.
  Future<void> setApiKey(String key) async {
    _cachedApiKey = key;
    // Write to keychain (primary)
    await _secureStorage?.write(key: AppConstants.apiKeyPref, value: key);
    // Also write to SharedPreferences for backward compatibility and sync reads
    // TODO: remove SharedPreferences fallback in a future version
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
  /// Defaults to platform-appropriate value when not set.
  String getLeaderKey() {
    final stored = _prefs?.getString(_leaderKeyPref);
    if (stored != null) return stored;
    // Platform default
    if (Platform.isMacOS) return 'cmd+x';
    return 'ctrl+x';
  }

  Future<void> setLeaderKey(String value) async {
    await _prefs?.setString(_leaderKeyPref, value);
  }

  /// Double-enter behavior: "steer", "interrupt", or "preempt".
  String getDoubleEnter() {
    return _prefs?.getString(_doubleEnterPref) ?? 'steer';
  }

  Future<void> setDoubleEnter(String value) async {
    await _prefs?.setString(_doubleEnterPref, value);
  }

  // ------ General helpers ------

  Future<bool> clearAll() async {
    await _secureStorage?.deleteAll();
    return await _prefs?.clear() ?? false;
  }

  bool containsKey(String key) => _prefs?.containsKey(key) ?? false;

  // ------ Internal keys ------

  static const String _hostPref = 'api_host';
  static const String _portPref = 'api_port';
  static const String _leaderKeyPref = 'leader_key';
  static const String _doubleEnterPref = 'double_enter';
}
