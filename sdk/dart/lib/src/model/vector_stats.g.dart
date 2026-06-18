// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'vector_stats.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$VectorStats extends VectorStats {
  @override
  final int loadedShards;
  @override
  final int maxRamShards;
  @override
  final int lruHits;
  @override
  final int lruMisses;
  @override
  final int lruEvictions;
  @override
  final String? shardDetails;

  factory _$VectorStats([void Function(VectorStatsBuilder)? updates]) =>
      (VectorStatsBuilder()..update(updates))._build();

  _$VectorStats._(
      {required this.loadedShards,
      required this.maxRamShards,
      required this.lruHits,
      required this.lruMisses,
      required this.lruEvictions,
      this.shardDetails})
      : super._();
  @override
  VectorStats rebuild(void Function(VectorStatsBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  VectorStatsBuilder toBuilder() => VectorStatsBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is VectorStats &&
        loadedShards == other.loadedShards &&
        maxRamShards == other.maxRamShards &&
        lruHits == other.lruHits &&
        lruMisses == other.lruMisses &&
        lruEvictions == other.lruEvictions &&
        shardDetails == other.shardDetails;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, loadedShards.hashCode);
    _$hash = $jc(_$hash, maxRamShards.hashCode);
    _$hash = $jc(_$hash, lruHits.hashCode);
    _$hash = $jc(_$hash, lruMisses.hashCode);
    _$hash = $jc(_$hash, lruEvictions.hashCode);
    _$hash = $jc(_$hash, shardDetails.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'VectorStats')
          ..add('loadedShards', loadedShards)
          ..add('maxRamShards', maxRamShards)
          ..add('lruHits', lruHits)
          ..add('lruMisses', lruMisses)
          ..add('lruEvictions', lruEvictions)
          ..add('shardDetails', shardDetails))
        .toString();
  }
}

class VectorStatsBuilder implements Builder<VectorStats, VectorStatsBuilder> {
  _$VectorStats? _$v;

  int? _loadedShards;
  int? get loadedShards => _$this._loadedShards;
  set loadedShards(int? loadedShards) => _$this._loadedShards = loadedShards;

  int? _maxRamShards;
  int? get maxRamShards => _$this._maxRamShards;
  set maxRamShards(int? maxRamShards) => _$this._maxRamShards = maxRamShards;

  int? _lruHits;
  int? get lruHits => _$this._lruHits;
  set lruHits(int? lruHits) => _$this._lruHits = lruHits;

  int? _lruMisses;
  int? get lruMisses => _$this._lruMisses;
  set lruMisses(int? lruMisses) => _$this._lruMisses = lruMisses;

  int? _lruEvictions;
  int? get lruEvictions => _$this._lruEvictions;
  set lruEvictions(int? lruEvictions) => _$this._lruEvictions = lruEvictions;

  String? _shardDetails;
  String? get shardDetails => _$this._shardDetails;
  set shardDetails(String? shardDetails) => _$this._shardDetails = shardDetails;

  VectorStatsBuilder() {
    VectorStats._defaults(this);
  }

  VectorStatsBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _loadedShards = $v.loadedShards;
      _maxRamShards = $v.maxRamShards;
      _lruHits = $v.lruHits;
      _lruMisses = $v.lruMisses;
      _lruEvictions = $v.lruEvictions;
      _shardDetails = $v.shardDetails;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(VectorStats other) {
    _$v = other as _$VectorStats;
  }

  @override
  void update(void Function(VectorStatsBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  VectorStats build() => _build();

  _$VectorStats _build() {
    final _$result = _$v ??
        _$VectorStats._(
          loadedShards: BuiltValueNullFieldError.checkNotNull(
              loadedShards, r'VectorStats', 'loadedShards'),
          maxRamShards: BuiltValueNullFieldError.checkNotNull(
              maxRamShards, r'VectorStats', 'maxRamShards'),
          lruHits: BuiltValueNullFieldError.checkNotNull(
              lruHits, r'VectorStats', 'lruHits'),
          lruMisses: BuiltValueNullFieldError.checkNotNull(
              lruMisses, r'VectorStats', 'lruMisses'),
          lruEvictions: BuiltValueNullFieldError.checkNotNull(
              lruEvictions, r'VectorStats', 'lruEvictions'),
          shardDetails: shardDetails,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
