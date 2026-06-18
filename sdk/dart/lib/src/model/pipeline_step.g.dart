// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_step.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineStep extends PipelineStep {
  @override
  final String id;
  @override
  final String name;
  @override
  final String status;
  @override
  final String? errorCommaOmitempty;
  @override
  final String? startedAtCommaOmitempty;
  @override
  final String? endedAtCommaOmitempty;

  factory _$PipelineStep([void Function(PipelineStepBuilder)? updates]) =>
      (PipelineStepBuilder()..update(updates))._build();

  _$PipelineStep._(
      {required this.id,
      required this.name,
      required this.status,
      this.errorCommaOmitempty,
      this.startedAtCommaOmitempty,
      this.endedAtCommaOmitempty})
      : super._();
  @override
  PipelineStep rebuild(void Function(PipelineStepBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineStepBuilder toBuilder() => PipelineStepBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PipelineStep &&
        id == other.id &&
        name == other.name &&
        status == other.status &&
        errorCommaOmitempty == other.errorCommaOmitempty &&
        startedAtCommaOmitempty == other.startedAtCommaOmitempty &&
        endedAtCommaOmitempty == other.endedAtCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, errorCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, startedAtCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, endedAtCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineStep')
          ..add('id', id)
          ..add('name', name)
          ..add('status', status)
          ..add('errorCommaOmitempty', errorCommaOmitempty)
          ..add('startedAtCommaOmitempty', startedAtCommaOmitempty)
          ..add('endedAtCommaOmitempty', endedAtCommaOmitempty))
        .toString();
  }
}

class PipelineStepBuilder
    implements Builder<PipelineStep, PipelineStepBuilder> {
  _$PipelineStep? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  String? _errorCommaOmitempty;
  String? get errorCommaOmitempty => _$this._errorCommaOmitempty;
  set errorCommaOmitempty(String? errorCommaOmitempty) =>
      _$this._errorCommaOmitempty = errorCommaOmitempty;

  String? _startedAtCommaOmitempty;
  String? get startedAtCommaOmitempty => _$this._startedAtCommaOmitempty;
  set startedAtCommaOmitempty(String? startedAtCommaOmitempty) =>
      _$this._startedAtCommaOmitempty = startedAtCommaOmitempty;

  String? _endedAtCommaOmitempty;
  String? get endedAtCommaOmitempty => _$this._endedAtCommaOmitempty;
  set endedAtCommaOmitempty(String? endedAtCommaOmitempty) =>
      _$this._endedAtCommaOmitempty = endedAtCommaOmitempty;

  PipelineStepBuilder() {
    PipelineStep._defaults(this);
  }

  PipelineStepBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _status = $v.status;
      _errorCommaOmitempty = $v.errorCommaOmitempty;
      _startedAtCommaOmitempty = $v.startedAtCommaOmitempty;
      _endedAtCommaOmitempty = $v.endedAtCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PipelineStep other) {
    _$v = other as _$PipelineStep;
  }

  @override
  void update(void Function(PipelineStepBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PipelineStep build() => _build();

  _$PipelineStep _build() {
    final _$result = _$v ??
        _$PipelineStep._(
          id: BuiltValueNullFieldError.checkNotNull(id, r'PipelineStep', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'PipelineStep', 'name'),
          status: BuiltValueNullFieldError.checkNotNull(
              status, r'PipelineStep', 'status'),
          errorCommaOmitempty: errorCommaOmitempty,
          startedAtCommaOmitempty: startedAtCommaOmitempty,
          endedAtCommaOmitempty: endedAtCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
