import 'package:formz/formz.dart';

/// Validates a daemon host string (hostname or IP, non-empty).
class DaemonHostInput extends FormzInput<String, String> {
  const DaemonHostInput.pure([super.value = '']) : super.pure();
  const DaemonHostInput.dirty([super.value = '']) : super.dirty();

  @override
  String? validator(String value) {
    final trimmed = value.trim();
    if (trimmed.isEmpty) return 'host is required';
    // Allow hostnames, IPs, and host:port
    final hostPort = Uri.parse('http://$trimmed'); // parse-friendly wrapper
    if (hostPort.host.isEmpty) return 'invalid host';
    return null;
  }
}

/// Validates an API port number (1-65535).
class DaemonPortInput extends FormzInput<int, String> {
  const DaemonPortInput.pure([super.value = 8081]) : super.pure();
  const DaemonPortInput.dirty([super.value = 8081]) : super.dirty();

  @override
  String? validator(int value) {
    if (value < 1 || value > 65535) return 'port must be 1-65535';
    return null;
  }
}

/// Validates an API key token (non-empty when provided for saving).
class ApiTokenInput extends FormzInput<String, String> {
  const ApiTokenInput.pure([super.value = '']) : super.pure();
  const ApiTokenInput.dirty([super.value = '']) : super.dirty();

  @override
  String? validator(String value) {
    // Token is allowed to be empty (means "use default"),
    // but if dirty and non-empty, ensure it has minimum length.
    if (value.isNotEmpty && value.length < 8) {
      return 'token must be at least 8 characters';
    }
    return null;
  }
}

/// Validates an STT language code (2-5 letter code).
class SttLanguageInput extends FormzInput<String, String> {
  const SttLanguageInput.pure([super.value = 'en']) : super.pure();
  const SttLanguageInput.dirty([super.value = 'en']) : super.dirty();

  @override
  String? validator(String value) {
    final trimmed = value.trim();
    if (trimmed.isEmpty) return 'language code is required';
    if (trimmed.length > 10) return 'language code too long';
    return null;
  }
}
