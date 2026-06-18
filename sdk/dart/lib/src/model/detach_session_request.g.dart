// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'detach_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$DetachSessionRequest extends DetachSessionRequest {
  @override
  final String id;
  @override
  final String agentId;

  factory _$DetachSessionRequest(
          [void Function(DetachSessionRequestBuilder)? updates]) =>
      (DetachSessionRequestBuilder()..update(updates))._build();

  _$DetachSessionRequest._({required this.id, required this.agentId})
      : super._();
  @override
  DetachSessionRequest rebuild(
          void Function(DetachSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  DetachSessionRequestBuilder toBuilder() =>
      DetachSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is DetachSessionRequest &&
        id == other.id &&
        agentId == other.agentId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, agentId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'DetachSessionRequest')
          ..add('id', id)
          ..add('agentId', agentId))
        .toString();
  }
}

class DetachSessionRequestBuilder
    implements Builder<DetachSessionRequest, DetachSessionRequestBuilder> {
  _$DetachSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _agentId;
  String? get agentId => _$this._agentId;
  set agentId(String? agentId) => _$this._agentId = agentId;

  DetachSessionRequestBuilder() {
    DetachSessionRequest._defaults(this);
  }

  DetachSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _agentId = $v.agentId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(DetachSessionRequest other) {
    _$v = other as _$DetachSessionRequest;
  }

  @override
  void update(void Function(DetachSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  DetachSessionRequest build() => _build();

  _$DetachSessionRequest _build() {
    final _$result = _$v ??
        _$DetachSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'DetachSessionRequest', 'id'),
          agentId: BuiltValueNullFieldError.checkNotNull(
              agentId, r'DetachSessionRequest', 'agentId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
