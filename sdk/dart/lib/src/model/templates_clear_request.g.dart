// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_clear_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesClearRequest extends TemplatesClearRequest {
  @override
  final String conversationId;
  @override
  final String? name;

  factory _$TemplatesClearRequest(
          [void Function(TemplatesClearRequestBuilder)? updates]) =>
      (TemplatesClearRequestBuilder()..update(updates))._build();

  _$TemplatesClearRequest._(
      {required this.conversationId, this.name})
      : super._();
  @override
  TemplatesClearRequest rebuild(
          void Function(TemplatesClearRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesClearRequestBuilder toBuilder() =>
      TemplatesClearRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesClearRequest &&
        conversationId == other.conversationId &&
        name == other.name;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, conversationId.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesClearRequest')
          ..add('conversationId', conversationId)
          ..add('name', name))
        .toString();
  }
}

class TemplatesClearRequestBuilder
    implements Builder<TemplatesClearRequest, TemplatesClearRequestBuilder> {
  _$TemplatesClearRequest? _$v;

  String? _conversationId;
  String? get conversationId => _$this._conversationId;
  set conversationId(String? conversationId) =>
      _$this._conversationId = conversationId;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) =>
      _$this._name = name;

  TemplatesClearRequestBuilder() {
    TemplatesClearRequest._defaults(this);
  }

  TemplatesClearRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _conversationId = $v.conversationId;
      _name = $v.name;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesClearRequest other) {
    _$v = other as _$TemplatesClearRequest;
  }

  @override
  void update(void Function(TemplatesClearRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesClearRequest build() => _build();

  _$TemplatesClearRequest _build() {
    final _$result = _$v ??
        _$TemplatesClearRequest._(
          conversationId: BuiltValueNullFieldError.checkNotNull(
              conversationId, r'TemplatesClearRequest', 'conversationId'),
          name: name,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
