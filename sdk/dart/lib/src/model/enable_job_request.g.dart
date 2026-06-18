// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'enable_job_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$EnableJobRequest extends EnableJobRequest {
  @override
  final String id;
  @override
  final bool enabled;

  factory _$EnableJobRequest(
          [void Function(EnableJobRequestBuilder)? updates]) =>
      (EnableJobRequestBuilder()..update(updates))._build();

  _$EnableJobRequest._({required this.id, required this.enabled}) : super._();
  @override
  EnableJobRequest rebuild(void Function(EnableJobRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  EnableJobRequestBuilder toBuilder() =>
      EnableJobRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is EnableJobRequest &&
        id == other.id &&
        enabled == other.enabled;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, enabled.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'EnableJobRequest')
          ..add('id', id)
          ..add('enabled', enabled))
        .toString();
  }
}

class EnableJobRequestBuilder
    implements Builder<EnableJobRequest, EnableJobRequestBuilder> {
  _$EnableJobRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  bool? _enabled;
  bool? get enabled => _$this._enabled;
  set enabled(bool? enabled) => _$this._enabled = enabled;

  EnableJobRequestBuilder() {
    EnableJobRequest._defaults(this);
  }

  EnableJobRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _enabled = $v.enabled;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(EnableJobRequest other) {
    _$v = other as _$EnableJobRequest;
  }

  @override
  void update(void Function(EnableJobRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  EnableJobRequest build() => _build();

  _$EnableJobRequest _build() {
    final _$result = _$v ??
        _$EnableJobRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'EnableJobRequest', 'id'),
          enabled: BuiltValueNullFieldError.checkNotNull(
              enabled, r'EnableJobRequest', 'enabled'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
