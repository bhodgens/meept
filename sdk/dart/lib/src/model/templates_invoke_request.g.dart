// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_invoke_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesInvokeRequest extends TemplatesInvokeRequest {
  @override
  final String name;
  @override
  final String? argsCommaOmitempty;

  factory _$TemplatesInvokeRequest(
          [void Function(TemplatesInvokeRequestBuilder)? updates]) =>
      (TemplatesInvokeRequestBuilder()..update(updates))._build();

  _$TemplatesInvokeRequest._({required this.name, this.argsCommaOmitempty})
      : super._();
  @override
  TemplatesInvokeRequest rebuild(
          void Function(TemplatesInvokeRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesInvokeRequestBuilder toBuilder() =>
      TemplatesInvokeRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesInvokeRequest &&
        name == other.name &&
        argsCommaOmitempty == other.argsCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, argsCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesInvokeRequest')
          ..add('name', name)
          ..add('argsCommaOmitempty', argsCommaOmitempty))
        .toString();
  }
}

class TemplatesInvokeRequestBuilder
    implements Builder<TemplatesInvokeRequest, TemplatesInvokeRequestBuilder> {
  _$TemplatesInvokeRequest? _$v;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _argsCommaOmitempty;
  String? get argsCommaOmitempty => _$this._argsCommaOmitempty;
  set argsCommaOmitempty(String? argsCommaOmitempty) =>
      _$this._argsCommaOmitempty = argsCommaOmitempty;

  TemplatesInvokeRequestBuilder() {
    TemplatesInvokeRequest._defaults(this);
  }

  TemplatesInvokeRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _argsCommaOmitempty = $v.argsCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesInvokeRequest other) {
    _$v = other as _$TemplatesInvokeRequest;
  }

  @override
  void update(void Function(TemplatesInvokeRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesInvokeRequest build() => _build();

  _$TemplatesInvokeRequest _build() {
    final _$result = _$v ??
        _$TemplatesInvokeRequest._(
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'TemplatesInvokeRequest', 'name'),
          argsCommaOmitempty: argsCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
