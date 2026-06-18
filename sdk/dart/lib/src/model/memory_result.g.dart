// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'memory_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$MemoryResult extends MemoryResult {
  @override
  final JsonObject memory;
  @override
  final num relevanceScore;
  @override
  final String source_;

  factory _$MemoryResult([void Function(MemoryResultBuilder)? updates]) =>
      (MemoryResultBuilder()..update(updates))._build();

  _$MemoryResult._(
      {required this.memory,
      required this.relevanceScore,
      required this.source_})
      : super._();
  @override
  MemoryResult rebuild(void Function(MemoryResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  MemoryResultBuilder toBuilder() => MemoryResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is MemoryResult &&
        memory == other.memory &&
        relevanceScore == other.relevanceScore &&
        source_ == other.source_;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, memory.hashCode);
    _$hash = $jc(_$hash, relevanceScore.hashCode);
    _$hash = $jc(_$hash, source_.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'MemoryResult')
          ..add('memory', memory)
          ..add('relevanceScore', relevanceScore)
          ..add('source_', source_))
        .toString();
  }
}

class MemoryResultBuilder
    implements Builder<MemoryResult, MemoryResultBuilder> {
  _$MemoryResult? _$v;

  JsonObject? _memory;
  JsonObject? get memory => _$this._memory;
  set memory(JsonObject? memory) => _$this._memory = memory;

  num? _relevanceScore;
  num? get relevanceScore => _$this._relevanceScore;
  set relevanceScore(num? relevanceScore) =>
      _$this._relevanceScore = relevanceScore;

  String? _source_;
  String? get source_ => _$this._source_;
  set source_(String? source_) => _$this._source_ = source_;

  MemoryResultBuilder() {
    MemoryResult._defaults(this);
  }

  MemoryResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _memory = $v.memory;
      _relevanceScore = $v.relevanceScore;
      _source_ = $v.source_;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(MemoryResult other) {
    _$v = other as _$MemoryResult;
  }

  @override
  void update(void Function(MemoryResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  MemoryResult build() => _build();

  _$MemoryResult _build() {
    final _$result = _$v ??
        _$MemoryResult._(
          memory: BuiltValueNullFieldError.checkNotNull(
              memory, r'MemoryResult', 'memory'),
          relevanceScore: BuiltValueNullFieldError.checkNotNull(
              relevanceScore, r'MemoryResult', 'relevanceScore'),
          source_: BuiltValueNullFieldError.checkNotNull(
              source_, r'MemoryResult', 'source_'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
