// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_invoke_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesInvokeResult extends TemplatesInvokeResult {
  @override
  final String prompt;
  @override
  final String? output;
  @override
  final bool success;
  @override
  final String? error;

  factory _$TemplatesInvokeResult(
          [void Function(TemplatesInvokeResultBuilder)? updates]) =>
      (TemplatesInvokeResultBuilder()..update(updates))._build();

  _$TemplatesInvokeResult._(
      {required this.prompt,
      this.output,
      required this.success,
      this.error})
      : super._();
  @override
  TemplatesInvokeResult rebuild(
          void Function(TemplatesInvokeResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesInvokeResultBuilder toBuilder() =>
      TemplatesInvokeResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesInvokeResult &&
        prompt == other.prompt &&
        output == other.output &&
        success == other.success &&
        error == other.error;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jc(_$hash, output.hashCode);
    _$hash = $jc(_$hash, success.hashCode);
    _$hash = $jc(_$hash, error.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesInvokeResult')
          ..add('prompt', prompt)
          ..add('output', output)
          ..add('success', success)
          ..add('error', error))
        .toString();
  }
}

class TemplatesInvokeResultBuilder
    implements Builder<TemplatesInvokeResult, TemplatesInvokeResultBuilder> {
  _$TemplatesInvokeResult? _$v;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  String? _output;
  String? get output => _$this._output;
  set output(String? output) =>
      _$this._output = output;

  bool? _success;
  bool? get success => _$this._success;
  set success(bool? success) => _$this._success = success;

  String? _error;
  String? get error => _$this._error;
  set error(String? error) =>
      _$this._error = error;

  TemplatesInvokeResultBuilder() {
    TemplatesInvokeResult._defaults(this);
  }

  TemplatesInvokeResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _prompt = $v.prompt;
      _output = $v.output;
      _success = $v.success;
      _error = $v.error;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesInvokeResult other) {
    _$v = other as _$TemplatesInvokeResult;
  }

  @override
  void update(void Function(TemplatesInvokeResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesInvokeResult build() => _build();

  _$TemplatesInvokeResult _build() {
    final _$result = _$v ??
        _$TemplatesInvokeResult._(
          prompt: BuiltValueNullFieldError.checkNotNull(
              prompt, r'TemplatesInvokeResult', 'prompt'),
          output: output,
          success: BuiltValueNullFieldError.checkNotNull(
              success, r'TemplatesInvokeResult', 'success'),
          error: error,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
