// First-run pairing screen (issue #59): shown when the app has NO API key
// (no stored key, no embedded dart-define key) — i.e. a distribution build
// meeting a daemon for the first time.
//
// Flow:
//   1. Desktop (kIsWeb false): try auto-pairing from the local
//      $HOME/.meept/dev_key file (same machine, MEEPT_HOME readable).
//   2. Otherwise ask for the one-time pairing code the daemon prints to its
//      console and exchange it via PairingService.
//   3. On success the key is stored and [onPaired] re-creates the
//      connection stack (WebSocketService refreshes the key from storage).
//
// All UI text is lowercase per the TUI/GUI convention.
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../services/pairing_service.dart';
import '../../services/storage_service.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Whether the first-run pairing gate should show for the current storage
/// state. Exposed so main.dart's gate stays declarative.
bool shouldShowPairingGate(StorageService storage) =>
    PairingService.needsPairing(storage);

/// Full-screen gate shown before the main app when pairing is needed.
class PairingGate extends ConsumerStatefulWidget {
  /// Called once pairing succeeded (or was not needed) — the parent swaps
  /// to the real app, whose providers re-read the key from storage.
  final VoidCallback onPaired;

  const PairingGate({super.key, required this.onPaired});

  @override
  ConsumerState<PairingGate> createState() => _PairingGateState();
}

class _PairingGateState extends ConsumerState<PairingGate> {
  final _codeController = TextEditingController();
  final _formKey = GlobalKey<FormState>();
  bool _busy = false;
  String? _error;
  bool _autoPairTried = false;

  @override
  void initState() {
    super.initState();
    // Desktop: auto-pair from the local dev key file before asking the
    // user for anything. Web skips this entirely (no filesystem).
    WidgetsBinding.instance.addPostFrameCallback((_) => _tryAutoPair());
  }

  @override
  void dispose() {
    _codeController.dispose();
    super.dispose();
  }

  Future<void> _tryAutoPair() async {
    if (_autoPairTried || !mounted) return;
    _autoPairTried = true;
    final storage = StorageService.instance;
    if (!storage.isInitialized) return; // web or failed storage: ask for code
    final pairing = PairingService(storage: storage);
    final paired = await pairing.autoPairFromLocalKeyFile();
    if (paired && mounted) {
      widget.onPaired();
    }
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate() || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    final pairing = PairingService(storage: StorageService.instance);
    final result = await pairing.exchange(_codeController.text);
    if (!mounted) return;
    setState(() {
      _busy = false;
      _error = result.success ? null : (result.error ?? 'pairing failed');
    });
    if (result.success) {
      widget.onPaired();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: CyberpunkColors.black,
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Form(
              key: _formKey,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    'pair with daemon',
                    style: CyberpunkTypography.headlineMedium.copyWith(
                      color: CyberpunkColors.orangePrimary,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    'this gui has no api key yet. run the meept daemon on '
                    'this machine and enter the one-time pairing code it '
                    'prints on startup.',
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.midGray,
                    ),
                  ),
                  const SizedBox(height: 24),
                  TextFormField(
                    controller: _codeController,
                    enabled: !_busy,
                    autofocus: true,
                    style: CyberpunkTypography.bodyLarge.copyWith(
                      color: CyberpunkColors.veryLightGray,
                      letterSpacing: 2,
                    ),
                    decoration: InputDecoration(
                      labelText: 'pairing code',
                      labelStyle: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.midGray,
                      ),
                      hintText: 'xxxx-xxxx-xxxx',
                      hintStyle: CyberpunkTypography.bodyMedium.copyWith(
                        color: CyberpunkColors.lightGray,
                      ),
                      border: const OutlineInputBorder(),
                      errorText: _error,
                    ),
                    validator: (v) => (v == null || v.trim().isEmpty)
                        ? 'enter the pairing code'
                        : null,
                    onFieldSubmitted: (_) => _submit(),
                  ),
                  const SizedBox(height: 16),
                  FilledButton(
                    onPressed: _busy ? null : _submit,
                    style: FilledButton.styleFrom(
                      backgroundColor: CyberpunkColors.orangePrimary,
                    ),
                    child: _busy
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : Text(
                            'pair',
                            style: CyberpunkTypography.bodyMedium.copyWith(
                              color: CyberpunkColors.black,
                            ),
                          ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
