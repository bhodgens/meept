// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'attach_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AttachSessionRequest extends AttachSessionRequest {
  @override
  final String id;
  @override
  final String agentId;

  factory _$AttachSessionRequest(
          [void Function(AttachSessionRequestBuilder)? updates]) =>
      (AttachSessionRequestBuilder()..update(updates))._build();

  _$AttachSessionRequest._({required this.id, required this.agentId})
      : super._();
  @override
  AttachSessionRequest rebuild(
          void Function(AttachSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AttachSessionRequestBuilder toBuilder() =>
      AttachSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AttachSessionRequest &&
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
    return (newBuiltValueToStringHelper(r'AttachSessionRequest')
          ..add('id', id)
          ..add('agentId', agentId))
        .toString();
  }
}

class AttachSessionRequestBuilder
    implements Builder<AttachSessionRequest, AttachSessionRequestBuilder> {
  _$AttachSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _agentId;
  String? get agentId => _$this._agentId;
  set agentId(String? agentId) => _$this._agentId = agentId;

  AttachSessionRequestBuilder() {
    AttachSessionRequest._defaults(this);
  }

  AttachSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _agentId = $v.agentId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AttachSessionRequest other) {
    _$v = other as _$AttachSessionRequest;
  }

  @override
  void update(void Function(AttachSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AttachSessionRequest build() => _build();

  _$AttachSessionRequest _build() {
    final _$result = _$v ??
        _$AttachSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'AttachSessionRequest', 'id'),
          agentId: BuiltValueNullFieldError.checkNotNull(
              agentId, r'AttachSessionRequest', 'agentId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
