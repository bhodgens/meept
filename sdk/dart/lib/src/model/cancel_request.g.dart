// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'cancel_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CancelRequest extends CancelRequest {
  @override
  final String cycleId;

  factory _$CancelRequest([void Function(CancelRequestBuilder)? updates]) =>
      (CancelRequestBuilder()..update(updates))._build();

  _$CancelRequest._({required this.cycleId}) : super._();
  @override
  CancelRequest rebuild(void Function(CancelRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CancelRequestBuilder toBuilder() => CancelRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CancelRequest && cycleId == other.cycleId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, cycleId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CancelRequest')
          ..add('cycleId', cycleId))
        .toString();
  }
}

class CancelRequestBuilder
    implements Builder<CancelRequest, CancelRequestBuilder> {
  _$CancelRequest? _$v;

  String? _cycleId;
  String? get cycleId => _$this._cycleId;
  set cycleId(String? cycleId) => _$this._cycleId = cycleId;

  CancelRequestBuilder() {
    CancelRequest._defaults(this);
  }

  CancelRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _cycleId = $v.cycleId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CancelRequest other) {
    _$v = other as _$CancelRequest;
  }

  @override
  void update(void Function(CancelRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CancelRequest build() => _build();

  _$CancelRequest _build() {
    final _$result = _$v ??
        _$CancelRequest._(
          cycleId: BuiltValueNullFieldError.checkNotNull(
              cycleId, r'CancelRequest', 'cycleId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
