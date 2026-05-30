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

  /// Whether [init] has been called and [_prefs] is populated.
  bool get isInitialized => _prefs != null;

  /// Initialize the underlying storage instances.
  /// Must be called (awaits) before any synchronous reads.
  Future<void> init() async {
    if (_prefs == null) {
      _prefs = await SharedPreferences.getInstance();
    }
    if (_secureStorage == null) {
      // Configure for macOS keychain
      _secureStorage = const FlutterSecureStorage(
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
    }
  }

  // ------ API Key (secure storage) ------

  /// Read API key from SharedPreferences synchronously.
  /// Keychain is written to but not read from synchronously.
  /// For keychain read, use getApiKeyAsync().
  String? getApiKey() {
    // After init(), prefs are available synchronously
    // Key writes go to keychain + prefs, so prefs always have the latest for sync read
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
    // Write to keychain (primary)
    await _secureStorage?.write(key: AppConstants.apiKeyPref, value: key);
    // Also write to SharedPreferences for backward compatibility and sync reads
    await _prefs?.setString(AppConstants.apiKeyPref, key);
  }

  /// Remove API key from both storage backends.
  Future<void> clearApiKey() async {
    await _secureStorage?.delete(key: AppConstants.apiKeyPref);
    await _prefs?.remove(AppConstants.apiKeyPref);
  }

  // ------ TLS Configuration ------

  /// Whether to use TLS (HTTPS/WSS) for connections.
  bool? getUseTls() => _prefs?.getBool(AppConstants.useTlsPref);

  Future<void> setUseTls(bool useTls) async {
    await _prefs?.setBool(AppConstants.useTlsPref, useTls);
  }

  // ------ Theme ------

  String? getTheme() => _prefs?.getString(AppConstants.themePref);

  Future<void> setTheme(String theme) async {
    await _prefs?.setString(AppConstants.themePref, theme);
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

  // ------ General helpers ------

  Future<bool> clearAll() async {
    await _secureStorage?.deleteAll();
    return await _prefs?.clear() ?? false;
  }

  bool containsKey(String key) => _prefs?.containsKey(key) ?? false;

  // ------ Internal keys ------

  static const String _hostPref = 'api_host';
  static const String _portPref = 'api_port';
}
