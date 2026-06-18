// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'cache_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CacheService extends CacheService {
  @override
  final JsonObject? cache;

  factory _$CacheService([void Function(CacheServiceBuilder)? updates]) =>
      (CacheServiceBuilder()..update(updates))._build();

  _$CacheService._({this.cache}) : super._();
  @override
  CacheService rebuild(void Function(CacheServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CacheServiceBuilder toBuilder() => CacheServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CacheService && cache == other.cache;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, cache.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CacheService')..add('cache', cache))
        .toString();
  }
}

class CacheServiceBuilder
    implements Builder<CacheService, CacheServiceBuilder> {
  _$CacheService? _$v;

  JsonObject? _cache;
  JsonObject? get cache => _$this._cache;
  set cache(JsonObject? cache) => _$this._cache = cache;

  CacheServiceBuilder() {
    CacheService._defaults(this);
  }

  CacheServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _cache = $v.cache;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CacheService other) {
    _$v = other as _$CacheService;
  }

  @override
  void update(void Function(CacheServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CacheService build() => _build();

  _$CacheService _build() {
    final _$result = _$v ??
        _$CacheService._(
          cache: cache,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
