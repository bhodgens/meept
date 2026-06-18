// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'runtime_status_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RuntimeStatusResponse extends RuntimeStatusResponse {
  @override
  final BuiltList<String>? runtimes;

  factory _$RuntimeStatusResponse(
          [void Function(RuntimeStatusResponseBuilder)? updates]) =>
      (RuntimeStatusResponseBuilder()..update(updates))._build();

  _$RuntimeStatusResponse._({this.runtimes}) : super._();
  @override
  RuntimeStatusResponse rebuild(
          void Function(RuntimeStatusResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RuntimeStatusResponseBuilder toBuilder() =>
      RuntimeStatusResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RuntimeStatusResponse && runtimes == other.runtimes;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, runtimes.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RuntimeStatusResponse')
          ..add('runtimes', runtimes))
        .toString();
  }
}

class RuntimeStatusResponseBuilder
    implements Builder<RuntimeStatusResponse, RuntimeStatusResponseBuilder> {
  _$RuntimeStatusResponse? _$v;

  ListBuilder<String>? _runtimes;
  ListBuilder<String> get runtimes =>
      _$this._runtimes ??= ListBuilder<String>();
  set runtimes(ListBuilder<String>? runtimes) => _$this._runtimes = runtimes;

  RuntimeStatusResponseBuilder() {
    RuntimeStatusResponse._defaults(this);
  }

  RuntimeStatusResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _runtimes = $v.runtimes?.toBuilder();
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RuntimeStatusResponse other) {
    _$v = other as _$RuntimeStatusResponse;
  }

  @override
  void update(void Function(RuntimeStatusResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RuntimeStatusResponse build() => _build();

  _$RuntimeStatusResponse _build() {
    _$RuntimeStatusResponse _$result;
    try {
      _$result = _$v ??
          _$RuntimeStatusResponse._(
            runtimes: _runtimes?.build(),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'runtimes';
        _runtimes?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'RuntimeStatusResponse', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
