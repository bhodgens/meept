// Conditional export for the external-URL open helper (deep links, leaf 03).
//
// Web builds get the dart:js_interop window-open implementation; native
// builds shell out to the platform opener. Import this file, never the
// halves directly. Same pattern as pairing_service.dart.
export 'platform_open_native.dart'
    if (dart.library.js_interop) 'platform_open_web.dart'
    if (dart.library.html) 'platform_open_web.dart';
