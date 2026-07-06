// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_pipeline_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreatePipelineRequest extends CreatePipelineRequest {
  @override
  final String? id;
  @override
  final String name;
  @override
  final String? description;
  @override
  final BuiltList<String>? steps;
  @override
  final String? metadata;

  factory _$CreatePipelineRequest(
          [void Function(CreatePipelineRequestBuilder)? updates]) =>
      (CreatePipelineRequestBuilder()..update(updates))._build();

  _$CreatePipelineRequest._(
      {this.id,
      required this.name,
      this.description,
      this.steps,
      this.metadata})
      : super._();
  @override
  CreatePipelineRequest rebuild(
          void Function(CreatePipelineRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CreatePipelineRequestBuilder toBuilder() =>
      CreatePipelineRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CreatePipelineRequest &&
        id == other.id &&
        name == other.name &&
        description == other.description &&
        steps == other.steps &&
        metadata == other.metadata;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, steps.hashCode);
    _$hash = $jc(_$hash, metadata.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreatePipelineRequest')
          ..add('id', id)
          ..add('name', name)
          ..add('description', description)
          ..add('steps', steps)
          ..add('metadata', metadata))
        .toString();
  }
}

class CreatePipelineRequestBuilder
    implements Builder<CreatePipelineRequest, CreatePipelineRequestBuilder> {
  _$CreatePipelineRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) =>
      _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) =>
      _$this._description = description;

  ListBuilder<String>? _steps;
  ListBuilder<String> get steps =>
      _$this._steps ??= ListBuilder<String>();
  set steps(ListBuilder<String>? steps) =>
      _$this._steps = steps;

  String? _metadata;
  String? get metadata => _$this._metadata;
  set metadata(String? metadata) =>
      _$this._metadata = metadata;

  CreatePipelineRequestBuilder() {
    CreatePipelineRequest._defaults(this);
  }

  CreatePipelineRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _description = $v.description;
      _steps = $v.steps?.toBuilder();
      _metadata = $v.metadata;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CreatePipelineRequest other) {
    _$v = other as _$CreatePipelineRequest;
  }

  @override
  void update(void Function(CreatePipelineRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CreatePipelineRequest build() => _build();

  _$CreatePipelineRequest _build() {
    _$CreatePipelineRequest _$result;
    try {
      _$result = _$v ??
          _$CreatePipelineRequest._(
            id: id,
            name: BuiltValueNullFieldError.checkNotNull(
                name, r'CreatePipelineRequest', 'name'),
            description: description,
            steps: _steps?.build(),
            metadata: metadata,
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'steps';
        _steps?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'CreatePipelineRequest', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
