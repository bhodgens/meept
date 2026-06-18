// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'remove_job_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RemoveJobRequest extends RemoveJobRequest {
  @override
  final String id;

  factory _$RemoveJobRequest(
          [void Function(RemoveJobRequestBuilder)? updates]) =>
      (RemoveJobRequestBuilder()..update(updates))._build();

  _$RemoveJobRequest._({required this.id}) : super._();
  @override
  RemoveJobRequest rebuild(void Function(RemoveJobRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RemoveJobRequestBuilder toBuilder() =>
      RemoveJobRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RemoveJobRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'RemoveJobRequest')..add('id', id))
        .toString();
  }
}

class RemoveJobRequestBuilder
    implements Builder<RemoveJobRequest, RemoveJobRequestBuilder> {
  _$RemoveJobRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  RemoveJobRequestBuilder() {
    RemoveJobRequest._defaults(this);
  }

  RemoveJobRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RemoveJobRequest other) {
    _$v = other as _$RemoveJobRequest;
  }

  @override
  void update(void Function(RemoveJobRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RemoveJobRequest build() => _build();

  _$RemoveJobRequest _build() {
    final _$result = _$v ??
        _$RemoveJobRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'RemoveJobRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
