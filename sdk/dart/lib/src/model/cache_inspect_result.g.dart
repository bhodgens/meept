// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'cache_inspect_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CacheInspectResult extends CacheInspectResult {
  @override
  final String promptHash;
  @override
  final String modelId;
  @override
  final String createdAt;
  @override
  final String expiresAt;
  @override
  final int hitCount;
  @override
  final String? fileHashesCommaOmitempty;
  @override
  final String source_;

  factory _$CacheInspectResult(
          [void Function(CacheInspectResultBuilder)? updates]) =>
      (CacheInspectResultBuilder()..update(updates))._build();

  _$CacheInspectResult._(
      {required this.promptHash,
      required this.modelId,
      required this.createdAt,
      required this.expiresAt,
      required this.hitCount,
      this.fileHashesCommaOmitempty,
      required this.source_})
      : super._();
  @override
  CacheInspectResult rebuild(
          void Function(CacheInspectResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CacheInspectResultBuilder toBuilder() =>
      CacheInspectResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CacheInspectResult &&
        promptHash == other.promptHash &&
        modelId == other.modelId &&
        createdAt == other.createdAt &&
        expiresAt == other.expiresAt &&
        hitCount == other.hitCount &&
        fileHashesCommaOmitempty == other.fileHashesCommaOmitempty &&
        source_ == other.source_;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, promptHash.hashCode);
    _$hash = $jc(_$hash, modelId.hashCode);
    _$hash = $jc(_$hash, createdAt.hashCode);
    _$hash = $jc(_$hash, expiresAt.hashCode);
    _$hash = $jc(_$hash, hitCount.hashCode);
    _$hash = $jc(_$hash, fileHashesCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, source_.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CacheInspectResult')
          ..add('promptHash', promptHash)
          ..add('modelId', modelId)
          ..add('createdAt', createdAt)
          ..add('expiresAt', expiresAt)
          ..add('hitCount', hitCount)
          ..add('fileHashesCommaOmitempty', fileHashesCommaOmitempty)
          ..add('source_', source_))
        .toString();
  }
}

class CacheInspectResultBuilder
    implements Builder<CacheInspectResult, CacheInspectResultBuilder> {
  _$CacheInspectResult? _$v;

  String? _promptHash;
  String? get promptHash => _$this._promptHash;
  set promptHash(String? promptHash) => _$this._promptHash = promptHash;

  String? _modelId;
  String? get modelId => _$this._modelId;
  set modelId(String? modelId) => _$this._modelId = modelId;

  String? _createdAt;
  String? get createdAt => _$this._createdAt;
  set createdAt(String? createdAt) => _$this._createdAt = createdAt;

  String? _expiresAt;
  String? get expiresAt => _$this._expiresAt;
  set expiresAt(String? expiresAt) => _$this._expiresAt = expiresAt;

  int? _hitCount;
  int? get hitCount => _$this._hitCount;
  set hitCount(int? hitCount) => _$this._hitCount = hitCount;

  String? _fileHashesCommaOmitempty;
  String? get fileHashesCommaOmitempty => _$this._fileHashesCommaOmitempty;
  set fileHashesCommaOmitempty(String? fileHashesCommaOmitempty) =>
      _$this._fileHashesCommaOmitempty = fileHashesCommaOmitempty;

  String? _source_;
  String? get source_ => _$this._source_;
  set source_(String? source_) => _$this._source_ = source_;

  CacheInspectResultBuilder() {
    CacheInspectResult._defaults(this);
  }

  CacheInspectResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _promptHash = $v.promptHash;
      _modelId = $v.modelId;
      _createdAt = $v.createdAt;
      _expiresAt = $v.expiresAt;
      _hitCount = $v.hitCount;
      _fileHashesCommaOmitempty = $v.fileHashesCommaOmitempty;
      _source_ = $v.source_;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CacheInspectResult other) {
    _$v = other as _$CacheInspectResult;
  }

  @override
  void update(void Function(CacheInspectResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CacheInspectResult build() => _build();

  _$CacheInspectResult _build() {
    final _$result = _$v ??
        _$CacheInspectResult._(
          promptHash: BuiltValueNullFieldError.checkNotNull(
              promptHash, r'CacheInspectResult', 'promptHash'),
          modelId: BuiltValueNullFieldError.checkNotNull(
              modelId, r'CacheInspectResult', 'modelId'),
          createdAt: BuiltValueNullFieldError.checkNotNull(
              createdAt, r'CacheInspectResult', 'createdAt'),
          expiresAt: BuiltValueNullFieldError.checkNotNull(
              expiresAt, r'CacheInspectResult', 'expiresAt'),
          hitCount: BuiltValueNullFieldError.checkNotNull(
              hitCount, r'CacheInspectResult', 'hitCount'),
          fileHashesCommaOmitempty: fileHashesCommaOmitempty,
          source_: BuiltValueNullFieldError.checkNotNull(
              source_, r'CacheInspectResult', 'source_'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
