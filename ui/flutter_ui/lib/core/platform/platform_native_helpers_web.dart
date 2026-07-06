// Web stub for platform_native_helpers
// All methods return null/false since web has no filesystem or OS access.

bool nativeIsMacOS() => false;
bool nativeIsLinux() => false;
bool nativeIsWindows() => false;
Future<String?> getHomeDirectoryNative() async => null;
Future<String?> readFileNative(String path) async => null;
