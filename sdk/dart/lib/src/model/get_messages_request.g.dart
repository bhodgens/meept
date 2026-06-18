// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'get_messages_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GetMessagesRequest extends GetMessagesRequest {
  @override
  final String id;
  @override
  final int? offsetCommaOmitempty;
  @override
  final int? limitCommaOmitempty;

  factory _$GetMessagesRequest(
          [void Function(GetMessagesRequestBuilder)? updates]) =>
      (GetMessagesRequestBuilder()..update(updates))._build();

  _$GetMessagesRequest._(
      {required this.id, this.offsetCommaOmitempty, this.limitCommaOmitempty})
      : super._();
  @override
  GetMessagesRequest rebuild(
          void Function(GetMessagesRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GetMessagesRequestBuilder toBuilder() =>
      GetMessagesRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GetMessagesRequest &&
        id == other.id &&
        offsetCommaOmitempty == other.offsetCommaOmitempty &&
        limitCommaOmitempty == other.limitCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, offsetCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'GetMessagesRequest')
          ..add('id', id)
          ..add('offsetCommaOmitempty', offsetCommaOmitempty)
          ..add('limitCommaOmitempty', limitCommaOmitempty))
        .toString();
  }
}

class GetMessagesRequestBuilder
    implements Builder<GetMessagesRequest, GetMessagesRequestBuilder> {
  _$GetMessagesRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  int? _offsetCommaOmitempty;
  int? get offsetCommaOmitempty => _$this._offsetCommaOmitempty;
  set offsetCommaOmitempty(int? offsetCommaOmitempty) =>
      _$this._offsetCommaOmitempty = offsetCommaOmitempty;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  GetMessagesRequestBuilder() {
    GetMessagesRequest._defaults(this);
  }

  GetMessagesRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _offsetCommaOmitempty = $v.offsetCommaOmitempty;
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GetMessagesRequest other) {
    _$v = other as _$GetMessagesRequest;
  }

  @override
  void update(void Function(GetMessagesRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GetMessagesRequest build() => _build();

  _$GetMessagesRequest _build() {
    final _$result = _$v ??
        _$GetMessagesRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'GetMessagesRequest', 'id'),
          offsetCommaOmitempty: offsetCommaOmitempty,
          limitCommaOmitempty: limitCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
