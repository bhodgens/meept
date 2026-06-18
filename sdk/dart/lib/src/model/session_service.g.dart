// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'session_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SessionService extends SessionService {
  @override
  final JsonObject? store;

  factory _$SessionService([void Function(SessionServiceBuilder)? updates]) =>
      (SessionServiceBuilder()..update(updates))._build();

  _$SessionService._({this.store}) : super._();
  @override
  SessionService rebuild(void Function(SessionServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SessionServiceBuilder toBuilder() => SessionServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SessionService && store == other.store;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, store.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SessionService')..add('store', store))
        .toString();
  }
}

class SessionServiceBuilder
    implements Builder<SessionService, SessionServiceBuilder> {
  _$SessionService? _$v;

  JsonObject? _store;
  JsonObject? get store => _$this._store;
  set store(JsonObject? store) => _$this._store = store;

  SessionServiceBuilder() {
    SessionService._defaults(this);
  }

  SessionServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _store = $v.store;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SessionService other) {
    _$v = other as _$SessionService;
  }

  @override
  void update(void Function(SessionServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SessionService build() => _build();

  _$SessionService _build() {
    final _$result = _$v ??
        _$SessionService._(
          store: store,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
