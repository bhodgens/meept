// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'branch_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$BranchSessionRequest extends BranchSessionRequest {
  @override
  final String id;
  @override
  final int targetMessageId;

  factory _$BranchSessionRequest(
          [void Function(BranchSessionRequestBuilder)? updates]) =>
      (BranchSessionRequestBuilder()..update(updates))._build();

  _$BranchSessionRequest._({required this.id, required this.targetMessageId})
      : super._();
  @override
  BranchSessionRequest rebuild(
          void Function(BranchSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  BranchSessionRequestBuilder toBuilder() =>
      BranchSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is BranchSessionRequest &&
        id == other.id &&
        targetMessageId == other.targetMessageId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, targetMessageId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'BranchSessionRequest')
          ..add('id', id)
          ..add('targetMessageId', targetMessageId))
        .toString();
  }
}

class BranchSessionRequestBuilder
    implements Builder<BranchSessionRequest, BranchSessionRequestBuilder> {
  _$BranchSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  int? _targetMessageId;
  int? get targetMessageId => _$this._targetMessageId;
  set targetMessageId(int? targetMessageId) =>
      _$this._targetMessageId = targetMessageId;

  BranchSessionRequestBuilder() {
    BranchSessionRequest._defaults(this);
  }

  BranchSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _targetMessageId = $v.targetMessageId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(BranchSessionRequest other) {
    _$v = other as _$BranchSessionRequest;
  }

  @override
  void update(void Function(BranchSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  BranchSessionRequest build() => _build();

  _$BranchSessionRequest _build() {
    final _$result = _$v ??
        _$BranchSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'BranchSessionRequest', 'id'),
          targetMessageId: BuiltValueNullFieldError.checkNotNull(
              targetMessageId, r'BranchSessionRequest', 'targetMessageId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
