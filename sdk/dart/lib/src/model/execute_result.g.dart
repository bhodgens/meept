// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'execute_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ExecuteResult extends ExecuteResult {
  @override
  final String output;
  @override
  final bool success;
  @override
  final String? errorCommaOmitempty;

  factory _$ExecuteResult([void Function(ExecuteResultBuilder)? updates]) =>
      (ExecuteResultBuilder()..update(updates))._build();

  _$ExecuteResult._(
      {required this.output, required this.success, this.errorCommaOmitempty})
      : super._();
  @override
  ExecuteResult rebuild(void Function(ExecuteResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ExecuteResultBuilder toBuilder() => ExecuteResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ExecuteResult &&
        output == other.output &&
        success == other.success &&
        errorCommaOmitempty == other.errorCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, output.hashCode);
    _$hash = $jc(_$hash, success.hashCode);
    _$hash = $jc(_$hash, errorCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ExecuteResult')
          ..add('output', output)
          ..add('success', success)
          ..add('errorCommaOmitempty', errorCommaOmitempty))
        .toString();
  }
}

class ExecuteResultBuilder
    implements Builder<ExecuteResult, ExecuteResultBuilder> {
  _$ExecuteResult? _$v;

  String? _output;
  String? get output => _$this._output;
  set output(String? output) => _$this._output = output;

  bool? _success;
  bool? get success => _$this._success;
  set success(bool? success) => _$this._success = success;

  String? _errorCommaOmitempty;
  String? get errorCommaOmitempty => _$this._errorCommaOmitempty;
  set errorCommaOmitempty(String? errorCommaOmitempty) =>
      _$this._errorCommaOmitempty = errorCommaOmitempty;

  ExecuteResultBuilder() {
    ExecuteResult._defaults(this);
  }

  ExecuteResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _output = $v.output;
      _success = $v.success;
      _errorCommaOmitempty = $v.errorCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ExecuteResult other) {
    _$v = other as _$ExecuteResult;
  }

  @override
  void update(void Function(ExecuteResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ExecuteResult build() => _build();

  _$ExecuteResult _build() {
    final _$result = _$v ??
        _$ExecuteResult._(
          output: BuiltValueNullFieldError.checkNotNull(
              output, r'ExecuteResult', 'output'),
          success: BuiltValueNullFieldError.checkNotNull(
              success, r'ExecuteResult', 'success'),
          errorCommaOmitempty: errorCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
