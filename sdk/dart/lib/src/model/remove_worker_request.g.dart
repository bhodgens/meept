// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'remove_worker_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RemoveWorkerRequest extends RemoveWorkerRequest {
  @override
  final String id;

  factory _$RemoveWorkerRequest(
          [void Function(RemoveWorkerRequestBuilder)? updates]) =>
      (RemoveWorkerRequestBuilder()..update(updates))._build();

  _$RemoveWorkerRequest._({required this.id}) : super._();
  @override
  RemoveWorkerRequest rebuild(
          void Function(RemoveWorkerRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RemoveWorkerRequestBuilder toBuilder() =>
      RemoveWorkerRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RemoveWorkerRequest && id == other.id;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RemoveWorkerRequest')..add('id', id))
        .toString();
  }
}

class RemoveWorkerRequestBuilder
    implements Builder<RemoveWorkerRequest, RemoveWorkerRequestBuilder> {
  _$RemoveWorkerRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  RemoveWorkerRequestBuilder() {
    RemoveWorkerRequest._defaults(this);
  }

  RemoveWorkerRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RemoveWorkerRequest other) {
    _$v = other as _$RemoveWorkerRequest;
  }

  @override
  void update(void Function(RemoveWorkerRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RemoveWorkerRequest build() => _build();

  _$RemoveWorkerRequest _build() {
    final _$result = _$v ??
        _$RemoveWorkerRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'RemoveWorkerRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
