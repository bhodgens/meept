// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_invoke_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesInvokeResult extends TemplatesInvokeResult {
  @override
  final String prompt;
  @override
  final String? outputCommaOmitempty;
  @override
  final bool success;
  @override
  final String? errorCommaOmitempty;

  factory _$TemplatesInvokeResult(
          [void Function(TemplatesInvokeResultBuilder)? updates]) =>
      (TemplatesInvokeResultBuilder()..update(updates))._build();

  _$TemplatesInvokeResult._(
      {required this.prompt,
      this.outputCommaOmitempty,
      required this.success,
      this.errorCommaOmitempty})
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
        outputCommaOmitempty == other.outputCommaOmitempty &&
        success == other.success &&
        errorCommaOmitempty == other.errorCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jc(_$hash, outputCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, success.hashCode);
    _$hash = $jc(_$hash, errorCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesInvokeResult')
          ..add('prompt', prompt)
          ..add('outputCommaOmitempty', outputCommaOmitempty)
          ..add('success', success)
          ..add('errorCommaOmitempty', errorCommaOmitempty))
        .toString();
  }
}

class TemplatesInvokeResultBuilder
    implements Builder<TemplatesInvokeResult, TemplatesInvokeResultBuilder> {
  _$TemplatesInvokeResult? _$v;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  String? _outputCommaOmitempty;
  String? get outputCommaOmitempty => _$this._outputCommaOmitempty;
  set outputCommaOmitempty(String? outputCommaOmitempty) =>
      _$this._outputCommaOmitempty = outputCommaOmitempty;

  bool? _success;
  bool? get success => _$this._success;
  set success(bool? success) => _$this._success = success;

  String? _errorCommaOmitempty;
  String? get errorCommaOmitempty => _$this._errorCommaOmitempty;
  set errorCommaOmitempty(String? errorCommaOmitempty) =>
      _$this._errorCommaOmitempty = errorCommaOmitempty;

  TemplatesInvokeResultBuilder() {
    TemplatesInvokeResult._defaults(this);
  }

  TemplatesInvokeResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _prompt = $v.prompt;
      _outputCommaOmitempty = $v.outputCommaOmitempty;
      _success = $v.success;
      _errorCommaOmitempty = $v.errorCommaOmitempty;
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
          outputCommaOmitempty: outputCommaOmitempty,
          success: BuiltValueNullFieldError.checkNotNull(
              success, r'TemplatesInvokeResult', 'success'),
          errorCommaOmitempty: errorCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
