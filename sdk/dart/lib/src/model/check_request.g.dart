// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'check_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CheckRequest extends CheckRequest {
  @override
  final String action;
  @override
  final String resource;

  factory _$CheckRequest([void Function(CheckRequestBuilder)? updates]) =>
      (CheckRequestBuilder()..update(updates))._build();

  _$CheckRequest._({required this.action, required this.resource}) : super._();
  @override
  CheckRequest rebuild(void Function(CheckRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CheckRequestBuilder toBuilder() => CheckRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CheckRequest &&
        action == other.action &&
        resource == other.resource;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, action.hashCode);
    _$hash = $jc(_$hash, resource.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CheckRequest')
          ..add('action', action)
          ..add('resource', resource))
        .toString();
  }
}

class CheckRequestBuilder
    implements Builder<CheckRequest, CheckRequestBuilder> {
  _$CheckRequest? _$v;

  String? _action;
  String? get action => _$this._action;
  set action(String? action) => _$this._action = action;

  String? _resource;
  String? get resource => _$this._resource;
  set resource(String? resource) => _$this._resource = resource;

  CheckRequestBuilder() {
    CheckRequest._defaults(this);
  }

  CheckRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _action = $v.action;
      _resource = $v.resource;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CheckRequest other) {
    _$v = other as _$CheckRequest;
  }

  @override
  void update(void Function(CheckRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CheckRequest build() => _build();

  _$CheckRequest _build() {
    final _$result = _$v ??
        _$CheckRequest._(
          action: BuiltValueNullFieldError.checkNotNull(
              action, r'CheckRequest', 'action'),
          resource: BuiltValueNullFieldError.checkNotNull(
              resource, r'CheckRequest', 'resource'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
