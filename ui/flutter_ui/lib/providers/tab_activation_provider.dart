import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../features/home/home_screen.dart' show HomeTab;

/// Set by child widgets (e.g. SessionsList) to request that HomeScreen
/// switch to a specific tab. HomeScreen watches this, applies the switch,
/// and clears it back to null. Null = no pending request.
final tabActivationProvider = StateProvider<HomeTab?>((ref) => null);

/// Provider for tracking which UI pane has keyboard focus.
/// Used for arrow-key navigation between list items.
///
/// - `focusedPane`: Which pane has focus (0 = left list, 1 = right content)
/// - `selectedIndex`: Current selected index in the list
class KeyboardFocusNotifier extends StateNotifier<KeyboardFocusState> {
  KeyboardFocusNotifier() : super(const KeyboardFocusState());

  void setFocusedPane(int pane) {
    state = state.copyWith(focusedPane: pane);
  }

  void setSelectedIndex(int index) {
    state = state.copyWith(selectedIndex: index);
  }

  void navigateUp(int maxIndex) {
    if (state.focusedPane != 0) return;
    final newIndex = (state.selectedIndex - 1).clamp(0, maxIndex);
    state = state.copyWith(selectedIndex: newIndex);
  }

  void navigateDown(int maxIndex) {
    if (state.focusedPane != 0) return;
    final newIndex = (state.selectedIndex + 1).clamp(0, maxIndex);
    state = state.copyWith(selectedIndex: newIndex);
  }

  void reset() {
    state = const KeyboardFocusState();
  }
}

class KeyboardFocusState {
  final int focusedPane; // 0 = left list, 1 = right content
  final int selectedIndex;

  const KeyboardFocusState({this.focusedPane = 0, this.selectedIndex = 0});

  KeyboardFocusState copyWith({int? focusedPane, int? selectedIndex}) {
    return KeyboardFocusState(
      focusedPane: focusedPane ?? this.focusedPane,
      selectedIndex: selectedIndex ?? this.selectedIndex,
    );
  }
}

final keyboardFocusProvider =
    StateNotifierProvider<KeyboardFocusNotifier, KeyboardFocusState>((ref) {
      return KeyboardFocusNotifier();
    });
