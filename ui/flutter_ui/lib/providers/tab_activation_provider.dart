import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../features/home/home_screen.dart' show HomeTab;

/// Set by child widgets (e.g. SessionsList) to request that HomeScreen
/// switch to a specific tab. HomeScreen watches this, applies the switch,
/// and clears it back to null. Null = no pending request.
final tabActivationProvider = StateProvider<HomeTab?>((ref) => null);
