import 'package:shared_preferences/shared_preferences.dart';
import '../core/constants.dart';

/// Centralized persistent storage backed by [SharedPreferences].
///
/// Provides typed get/set methods for API key, theme, daemon host, etc.
/// The service is a singleton that must be initialized via [init] in
/// [main] before any synchronous reads are performed.  Once [init] has
/// completed, all subsequent getter calls are synchronous.
class StorageService {
  static final StorageService _instance = StorageService._();

  StorageService._();

  /// Global singleton accessor.  Call [init] once at app startup.
  static StorageService get instance => _instance;

  SharedPreferences? _prefs;

  /// Whether [init] has been called and [_prefs] is populated.
  bool get isInitialized => _prefs != null;

  /// Initialize the underlying [SharedPreferences] instance.
  /// Must be called (awaits) before any synchronous reads.
  Future<void> init() async {
    if (_prefs == null) {
      _prefs = await SharedPreferences.getInstance();
    }
  }

  // ------ API Key ------

  /// Synchronous read — ensure [init] has completed first.
  String? getApiKey() => _prefs?.getString(AppConstants.apiKeyPref);

  Future<void> setApiKey(String key) async {
    await _prefs?.setString(AppConstants.apiKeyPref, key);
  }

  Future<void> clearApiKey() async {
    await _prefs?.remove(AppConstants.apiKeyPref);
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
    return await _prefs?.clear() ?? false;
  }

  bool containsKey(String key) => _prefs?.containsKey(key) ?? false;

  // ------ Internal keys ------

  static const String _hostPref = 'api_host';
  static const String _portPref = 'api_port';
}
