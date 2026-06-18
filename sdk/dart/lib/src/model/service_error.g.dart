// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'service_error.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ServiceError extends ServiceError {
  @override
  final String? service;
  @override
  final String? op;
  @override
  final JsonObject? err;

  factory _$ServiceError([void Function(ServiceErrorBuilder)? updates]) =>
      (ServiceErrorBuilder()..update(updates))._build();

  _$ServiceError._({this.service, this.op, this.err}) : super._();
  @override
  ServiceError rebuild(void Function(ServiceErrorBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ServiceErrorBuilder toBuilder() => ServiceErrorBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ServiceError &&
        service == other.service &&
        op == other.op &&
        err == other.err;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, service.hashCode);
    _$hash = $jc(_$hash, op.hashCode);
    _$hash = $jc(_$hash, err.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ServiceError')
          ..add('service', service)
          ..add('op', op)
          ..add('err', err))
        .toString();
  }
}

class ServiceErrorBuilder
    implements Builder<ServiceError, ServiceErrorBuilder> {
  _$ServiceError? _$v;

  String? _service;
  String? get service => _$this._service;
  set service(String? service) => _$this._service = service;

  String? _op;
  String? get op => _$this._op;
  set op(String? op) => _$this._op = op;

  JsonObject? _err;
  JsonObject? get err => _$this._err;
  set err(JsonObject? err) => _$this._err = err;

  ServiceErrorBuilder() {
    ServiceError._defaults(this);
  }

  ServiceErrorBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _service = $v.service;
      _op = $v.op;
      _err = $v.err;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ServiceError other) {
    _$v = other as _$ServiceError;
  }

  @override
  void update(void Function(ServiceErrorBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ServiceError build() => _build();

  _$ServiceError _build() {
    final _$result = _$v ??
        _$ServiceError._(
          service: service,
          op: op,
          err: err,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
