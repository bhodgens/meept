// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_get_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesGetRequest extends TemplatesGetRequest {
  @override
  final String name;

  factory _$TemplatesGetRequest(
          [void Function(TemplatesGetRequestBuilder)? updates]) =>
      (TemplatesGetRequestBuilder()..update(updates))._build();

  _$TemplatesGetRequest._({required this.name}) : super._();
  @override
  TemplatesGetRequest rebuild(
          void Function(TemplatesGetRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesGetRequestBuilder toBuilder() =>
      TemplatesGetRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesGetRequest && name == other.name;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesGetRequest')
          ..add('name', name))
        .toString();
  }
}

class TemplatesGetRequestBuilder
    implements Builder<TemplatesGetRequest, TemplatesGetRequestBuilder> {
  _$TemplatesGetRequest? _$v;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  TemplatesGetRequestBuilder() {
    TemplatesGetRequest._defaults(this);
  }

  TemplatesGetRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesGetRequest other) {
    _$v = other as _$TemplatesGetRequest;
  }

  @override
  void update(void Function(TemplatesGetRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesGetRequest build() => _build();

  _$TemplatesGetRequest _build() {
    final _$result = _$v ??
        _$TemplatesGetRequest._(
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'TemplatesGetRequest', 'name'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
