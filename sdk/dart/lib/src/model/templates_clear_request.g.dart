// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_clear_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesClearRequest extends TemplatesClearRequest {
  @override
  final String conversationId;
  @override
  final String? nameCommaOmitempty;

  factory _$TemplatesClearRequest(
          [void Function(TemplatesClearRequestBuilder)? updates]) =>
      (TemplatesClearRequestBuilder()..update(updates))._build();

  _$TemplatesClearRequest._(
      {required this.conversationId, this.nameCommaOmitempty})
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
        nameCommaOmitempty == other.nameCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, conversationId.hashCode);
    _$hash = $jc(_$hash, nameCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesClearRequest')
          ..add('conversationId', conversationId)
          ..add('nameCommaOmitempty', nameCommaOmitempty))
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

  String? _nameCommaOmitempty;
  String? get nameCommaOmitempty => _$this._nameCommaOmitempty;
  set nameCommaOmitempty(String? nameCommaOmitempty) =>
      _$this._nameCommaOmitempty = nameCommaOmitempty;

  TemplatesClearRequestBuilder() {
    TemplatesClearRequest._defaults(this);
  }

  TemplatesClearRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _conversationId = $v.conversationId;
      _nameCommaOmitempty = $v.nameCommaOmitempty;
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
          nameCommaOmitempty: nameCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
