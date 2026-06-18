// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_status_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineStatusResponse extends PipelineStatusResponse {
  @override
  final String pipelineId;
  @override
  final String name;
  @override
  final String status;
  @override
  final BuiltList<String>? steps;
  @override
  final String createdAt;
  @override
  final String updatedAt;

  factory _$PipelineStatusResponse(
          [void Function(PipelineStatusResponseBuilder)? updates]) =>
      (PipelineStatusResponseBuilder()..update(updates))._build();

  _$PipelineStatusResponse._(
      {required this.pipelineId,
      required this.name,
      required this.status,
      this.steps,
      required this.createdAt,
      required this.updatedAt})
      : super._();
  @override
  PipelineStatusResponse rebuild(
          void Function(PipelineStatusResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineStatusResponseBuilder toBuilder() =>
      PipelineStatusResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PipelineStatusResponse &&
        pipelineId == other.pipelineId &&
        name == other.name &&
        status == other.status &&
        steps == other.steps &&
        createdAt == other.createdAt &&
        updatedAt == other.updatedAt;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pipelineId.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, steps.hashCode);
    _$hash = $jc(_$hash, createdAt.hashCode);
    _$hash = $jc(_$hash, updatedAt.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineStatusResponse')
          ..add('pipelineId', pipelineId)
          ..add('name', name)
          ..add('status', status)
          ..add('steps', steps)
          ..add('createdAt', createdAt)
          ..add('updatedAt', updatedAt))
        .toString();
  }
}

class PipelineStatusResponseBuilder
    implements Builder<PipelineStatusResponse, PipelineStatusResponseBuilder> {
  _$PipelineStatusResponse? _$v;

  String? _pipelineId;
  String? get pipelineId => _$this._pipelineId;
  set pipelineId(String? pipelineId) => _$this._pipelineId = pipelineId;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  ListBuilder<String>? _steps;
  ListBuilder<String> get steps => _$this._steps ??= ListBuilder<String>();
  set steps(ListBuilder<String>? steps) => _$this._steps = steps;

  String? _createdAt;
  String? get createdAt => _$this._createdAt;
  set createdAt(String? createdAt) => _$this._createdAt = createdAt;

  String? _updatedAt;
  String? get updatedAt => _$this._updatedAt;
  set updatedAt(String? updatedAt) => _$this._updatedAt = updatedAt;

  PipelineStatusResponseBuilder() {
    PipelineStatusResponse._defaults(this);
  }

  PipelineStatusResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pipelineId = $v.pipelineId;
      _name = $v.name;
      _status = $v.status;
      _steps = $v.steps?.toBuilder();
      _createdAt = $v.createdAt;
      _updatedAt = $v.updatedAt;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PipelineStatusResponse other) {
    _$v = other as _$PipelineStatusResponse;
  }

  @override
  void update(void Function(PipelineStatusResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PipelineStatusResponse build() => _build();

  _$PipelineStatusResponse _build() {
    _$PipelineStatusResponse _$result;
    try {
      _$result = _$v ??
          _$PipelineStatusResponse._(
            pipelineId: BuiltValueNullFieldError.checkNotNull(
                pipelineId, r'PipelineStatusResponse', 'pipelineId'),
            name: BuiltValueNullFieldError.checkNotNull(
                name, r'PipelineStatusResponse', 'name'),
            status: BuiltValueNullFieldError.checkNotNull(
                status, r'PipelineStatusResponse', 'status'),
            steps: _steps?.build(),
            createdAt: BuiltValueNullFieldError.checkNotNull(
                createdAt, r'PipelineStatusResponse', 'createdAt'),
            updatedAt: BuiltValueNullFieldError.checkNotNull(
                updatedAt, r'PipelineStatusResponse', 'updatedAt'),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'steps';
        _steps?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'PipelineStatusResponse', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
