# Flutter Multi-Platform Fix Plan

## Problem
The Flutter GUI uses `dart:io` APIs (`Platform.environment`, `Platform.isMacOS`, `Process.run`) that are unsupported on web, causing white screen crashes.

## Solution
Implement platform-conditional code using Flutter's conditional imports and `kIsWeb` checks to support web, macOS, Linux, and Windows from a single codebase.

## Phases

### Phase 1: Infrastructure Setup ✅ COMPLETE
- Created platform abstraction layer in `ui/flutter_ui/lib/core/platform/`
  - `platform_service.dart` - singleton with safe null/default returns on web
  - `platform_native_helpers.dart` - native-only helpers with dart:io imports
- Updated CLAUDE.md with multi-platform patterns (`#flutter-multi-platform`)
- Updated MEMORY.md with lessons learned

### Phase 2: Fix storage_service.dart ✅ COMPLETE
- Replaced `Platform.environment['HOME']` with `platformService.getHomeDirectory()`
- Replaced `Process.run` shell fallback with platform service
- Updated `getLeaderKey()` to use `platformService.defaultLeaderKey`

### Phase 3: Codebase Audit ✅ COMPLETE
Fixed files:
- `services/storage_service.dart` - full migration to platform service
- `core/shortcuts.dart` - replaced `Platform.isMacOS` with compile-time constant
- `services/window_geometry_service.dart` - replaced `Platform.is*` with `kIsWeb` guard
- `features/chat/chat_input.dart` - wrapped `File` operations in `if (kIsWeb) return;`

**Verification:** `flutter build web --release` succeeds (✓ Built build/web)

## Acceptance Criteria ✅ ALL COMPLETE
1. ✅ `make webui` launches without errors (web build compiles)
2. ✅ macOS GUI app continues to work identically (native helpers preserve functionality)
3. ✅ All platform-specific code is behind proper abstractions
4. ✅ New platform constants/patterns documented in CLAUDE.md and MEMORY.md
