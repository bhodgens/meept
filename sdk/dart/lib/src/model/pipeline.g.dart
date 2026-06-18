// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$Pipeline extends Pipeline {
  @override
  final String id;
  @override
  final String name;
  @override
  final String description;
  @override
  final String status;
  @override
  final BuiltList<String>? steps;
  @override
  final String? metadata;
  @override
  final String createdAt;
  @override
  final String updatedAt;

  factory _$Pipeline([void Function(PipelineBuilder)? updates]) =>
      (PipelineBuilder()..update(updates))._build();

  _$Pipeline._(
      {required this.id,
      required this.name,
      required this.description,
      required this.status,
      this.steps,
      this.metadata,
      required this.createdAt,
      required this.updatedAt})
      : super._();
  @override
  Pipeline rebuild(void Function(PipelineBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineBuilder toBuilder() => PipelineBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is Pipeline &&
        id == other.id &&
        name == other.name &&
        description == other.description &&
        status == other.status &&
        steps == other.steps &&
        metadata == other.metadata &&
        createdAt == other.createdAt &&
        updatedAt == other.updatedAt;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, steps.hashCode);
    _$hash = $jc(_$hash, metadata.hashCode);
    _$hash = $jc(_$hash, createdAt.hashCode);
    _$hash = $jc(_$hash, updatedAt.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'Pipeline')
          ..add('id', id)
          ..add('name', name)
          ..add('description', description)
          ..add('status', status)
          ..add('steps', steps)
          ..add('metadata', metadata)
          ..add('createdAt', createdAt)
          ..add('updatedAt', updatedAt))
        .toString();
  }
}

class PipelineBuilder implements Builder<Pipeline, PipelineBuilder> {
  _$Pipeline? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) => _$this._description = description;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  ListBuilder<String>? _steps;
  ListBuilder<String> get steps => _$this._steps ??= ListBuilder<String>();
  set steps(ListBuilder<String>? steps) => _$this._steps = steps;

  String? _metadata;
  String? get metadata => _$this._metadata;
  set metadata(String? metadata) => _$this._metadata = metadata;

  String? _createdAt;
  String? get createdAt => _$this._createdAt;
  set createdAt(String? createdAt) => _$this._createdAt = createdAt;

  String? _updatedAt;
  String? get updatedAt => _$this._updatedAt;
  set updatedAt(String? updatedAt) => _$this._updatedAt = updatedAt;

  PipelineBuilder() {
    Pipeline._defaults(this);
  }

  PipelineBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _description = $v.description;
      _status = $v.status;
      _steps = $v.steps?.toBuilder();
      _metadata = $v.metadata;
      _createdAt = $v.createdAt;
      _updatedAt = $v.updatedAt;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(Pipeline other) {
    _$v = other as _$Pipeline;
  }

  @override
  void update(void Function(PipelineBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  Pipeline build() => _build();

  _$Pipeline _build() {
    _$Pipeline _$result;
    try {
      _$result = _$v ??
          _$Pipeline._(
            id: BuiltValueNullFieldError.checkNotNull(id, r'Pipeline', 'id'),
            name: BuiltValueNullFieldError.checkNotNull(
                name, r'Pipeline', 'name'),
            description: BuiltValueNullFieldError.checkNotNull(
                description, r'Pipeline', 'description'),
            status: BuiltValueNullFieldError.checkNotNull(
                status, r'Pipeline', 'status'),
            steps: _steps?.build(),
            metadata: metadata,
            createdAt: BuiltValueNullFieldError.checkNotNull(
                createdAt, r'Pipeline', 'createdAt'),
            updatedAt: BuiltValueNullFieldError.checkNotNull(
                updatedAt, r'Pipeline', 'updatedAt'),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'steps';
        _steps?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'Pipeline', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
