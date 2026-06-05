import 'package:freezed_annotation/freezed_annotation.dart';

part 'async_state.freezed.dart';

@freezed
class AsyncState<T> with _$AsyncState<T> {
  const factory AsyncState.initial() = _Initial;
  const factory AsyncState.loading() = _Loading;
  const factory AsyncState.data(T value) = _Data;
  const factory AsyncState.error(Object error, StackTrace stackTrace) = _Error;
}
