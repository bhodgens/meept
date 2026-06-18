// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'shard_detail.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ShardDetail extends ShardDetail {
  @override
  final int dimension;
  @override
  final int m;
  @override
  final int efConstruction;
  @override
  final int efSearch;
  @override
  final int vectorCount;
  @override
  final int databaseSizeBytes;
  @override
  final String shardId;

  factory _$ShardDetail([void Function(ShardDetailBuilder)? updates]) =>
      (ShardDetailBuilder()..update(updates))._build();

  _$ShardDetail._(
      {required this.dimension,
      required this.m,
      required this.efConstruction,
      required this.efSearch,
      required this.vectorCount,
      required this.databaseSizeBytes,
      required this.shardId})
      : super._();
  @override
  ShardDetail rebuild(void Function(ShardDetailBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ShardDetailBuilder toBuilder() => ShardDetailBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ShardDetail &&
        dimension == other.dimension &&
        m == other.m &&
        efConstruction == other.efConstruction &&
        efSearch == other.efSearch &&
        vectorCount == other.vectorCount &&
        databaseSizeBytes == other.databaseSizeBytes &&
        shardId == other.shardId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, dimension.hashCode);
    _$hash = $jc(_$hash, m.hashCode);
    _$hash = $jc(_$hash, efConstruction.hashCode);
    _$hash = $jc(_$hash, efSearch.hashCode);
    _$hash = $jc(_$hash, vectorCount.hashCode);
    _$hash = $jc(_$hash, databaseSizeBytes.hashCode);
    _$hash = $jc(_$hash, shardId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ShardDetail')
          ..add('dimension', dimension)
          ..add('m', m)
          ..add('efConstruction', efConstruction)
          ..add('efSearch', efSearch)
          ..add('vectorCount', vectorCount)
          ..add('databaseSizeBytes', databaseSizeBytes)
          ..add('shardId', shardId))
        .toString();
  }
}

class ShardDetailBuilder implements Builder<ShardDetail, ShardDetailBuilder> {
  _$ShardDetail? _$v;

  int? _dimension;
  int? get dimension => _$this._dimension;
  set dimension(int? dimension) => _$this._dimension = dimension;

  int? _m;
  int? get m => _$this._m;
  set m(int? m) => _$this._m = m;

  int? _efConstruction;
  int? get efConstruction => _$this._efConstruction;
  set efConstruction(int? efConstruction) =>
      _$this._efConstruction = efConstruction;

  int? _efSearch;
  int? get efSearch => _$this._efSearch;
  set efSearch(int? efSearch) => _$this._efSearch = efSearch;

  int? _vectorCount;
  int? get vectorCount => _$this._vectorCount;
  set vectorCount(int? vectorCount) => _$this._vectorCount = vectorCount;

  int? _databaseSizeBytes;
  int? get databaseSizeBytes => _$this._databaseSizeBytes;
  set databaseSizeBytes(int? databaseSizeBytes) =>
      _$this._databaseSizeBytes = databaseSizeBytes;

  String? _shardId;
  String? get shardId => _$this._shardId;
  set shardId(String? shardId) => _$this._shardId = shardId;

  ShardDetailBuilder() {
    ShardDetail._defaults(this);
  }

  ShardDetailBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _dimension = $v.dimension;
      _m = $v.m;
      _efConstruction = $v.efConstruction;
      _efSearch = $v.efSearch;
      _vectorCount = $v.vectorCount;
      _databaseSizeBytes = $v.databaseSizeBytes;
      _shardId = $v.shardId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ShardDetail other) {
    _$v = other as _$ShardDetail;
  }

  @override
  void update(void Function(ShardDetailBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ShardDetail build() => _build();

  _$ShardDetail _build() {
    final _$result = _$v ??
        _$ShardDetail._(
          dimension: BuiltValueNullFieldError.checkNotNull(
              dimension, r'ShardDetail', 'dimension'),
          m: BuiltValueNullFieldError.checkNotNull(m, r'ShardDetail', 'm'),
          efConstruction: BuiltValueNullFieldError.checkNotNull(
              efConstruction, r'ShardDetail', 'efConstruction'),
          efSearch: BuiltValueNullFieldError.checkNotNull(
              efSearch, r'ShardDetail', 'efSearch'),
          vectorCount: BuiltValueNullFieldError.checkNotNull(
              vectorCount, r'ShardDetail', 'vectorCount'),
          databaseSizeBytes: BuiltValueNullFieldError.checkNotNull(
              databaseSizeBytes, r'ShardDetail', 'databaseSizeBytes'),
          shardId: BuiltValueNullFieldError.checkNotNull(
              shardId, r'ShardDetail', 'shardId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
