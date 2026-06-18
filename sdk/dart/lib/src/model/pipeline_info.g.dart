// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineInfo extends PipelineInfo {
  @override
  final String id;
  @override
  final String name;
  @override
  final String status;
  @override
  final String createdAt;

  factory _$PipelineInfo([void Function(PipelineInfoBuilder)? updates]) =>
      (PipelineInfoBuilder()..update(updates))._build();

  _$PipelineInfo._(
      {required this.id,
      required this.name,
      required this.status,
      required this.createdAt})
      : super._();
  @override
  PipelineInfo rebuild(void Function(PipelineInfoBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineInfoBuilder toBuilder() => PipelineInfoBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PipelineInfo &&
        id == other.id &&
        name == other.name &&
        status == other.status &&
        createdAt == other.createdAt;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, createdAt.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineInfo')
          ..add('id', id)
          ..add('name', name)
          ..add('status', status)
          ..add('createdAt', createdAt))
        .toString();
  }
}

class PipelineInfoBuilder
    implements Builder<PipelineInfo, PipelineInfoBuilder> {
  _$PipelineInfo? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  String? _createdAt;
  String? get createdAt => _$this._createdAt;
  set createdAt(String? createdAt) => _$this._createdAt = createdAt;

  PipelineInfoBuilder() {
    PipelineInfo._defaults(this);
  }

  PipelineInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _status = $v.status;
      _createdAt = $v.createdAt;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PipelineInfo other) {
    _$v = other as _$PipelineInfo;
  }

  @override
  void update(void Function(PipelineInfoBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PipelineInfo build() => _build();

  _$PipelineInfo _build() {
    final _$result = _$v ??
        _$PipelineInfo._(
          id: BuiltValueNullFieldError.checkNotNull(id, r'PipelineInfo', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'PipelineInfo', 'name'),
          status: BuiltValueNullFieldError.checkNotNull(
              status, r'PipelineInfo', 'status'),
          createdAt: BuiltValueNullFieldError.checkNotNull(
              createdAt, r'PipelineInfo', 'createdAt'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
