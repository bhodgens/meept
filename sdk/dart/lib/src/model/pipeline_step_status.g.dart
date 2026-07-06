// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_step_status.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineStepStatus extends PipelineStepStatus {
  @override
  final String id;
  @override
  final String name;
  @override
  final String status;
  @override
  final String? error;
  @override
  final String? startedAt;
  @override
  final String? endedAt;

  factory _$PipelineStepStatus(
          [void Function(PipelineStepStatusBuilder)? updates]) =>
      (PipelineStepStatusBuilder()..update(updates))._build();

  _$PipelineStepStatus._(
      {required this.id,
      required this.name,
      required this.status,
      this.error,
      this.startedAt,
      this.endedAt})
      : super._();
  @override
  PipelineStepStatus rebuild(
          void Function(PipelineStepStatusBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineStepStatusBuilder toBuilder() =>
      PipelineStepStatusBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PipelineStepStatus &&
        id == other.id &&
        name == other.name &&
        status == other.status &&
        error == other.error &&
        startedAt == other.startedAt &&
        endedAt == other.endedAt;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, error.hashCode);
    _$hash = $jc(_$hash, startedAt.hashCode);
    _$hash = $jc(_$hash, endedAt.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineStepStatus')
          ..add('id', id)
          ..add('name', name)
          ..add('status', status)
          ..add('error', error)
          ..add('startedAt', startedAt)
          ..add('endedAt', endedAt))
        .toString();
  }
}

class PipelineStepStatusBuilder
    implements Builder<PipelineStepStatus, PipelineStepStatusBuilder> {
  _$PipelineStepStatus? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  String? _error;
  String? get error => _$this._error;
  set error(String? error) =>
      _$this._error = error;

  String? _startedAt;
  String? get startedAt => _$this._startedAt;
  set startedAt(String? startedAt) =>
      _$this._startedAt = startedAt;

  String? _endedAt;
  String? get endedAt => _$this._endedAt;
  set endedAt(String? endedAt) =>
      _$this._endedAt = endedAt;

  PipelineStepStatusBuilder() {
    PipelineStepStatus._defaults(this);
  }

  PipelineStepStatusBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _status = $v.status;
      _error = $v.error;
      _startedAt = $v.startedAt;
      _endedAt = $v.endedAt;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PipelineStepStatus other) {
    _$v = other as _$PipelineStepStatus;
  }

  @override
  void update(void Function(PipelineStepStatusBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PipelineStepStatus build() => _build();

  _$PipelineStepStatus _build() {
    final _$result = _$v ??
        _$PipelineStepStatus._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'PipelineStepStatus', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'PipelineStepStatus', 'name'),
          status: BuiltValueNullFieldError.checkNotNull(
              status, r'PipelineStepStatus', 'status'),
          error: error,
          startedAt: startedAt,
          endedAt: endedAt,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
