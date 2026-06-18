// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_pipeline_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreatePipelineRequest extends CreatePipelineRequest {
  @override
  final String? idCommaOmitempty;
  @override
  final String name;
  @override
  final String? descriptionCommaOmitempty;
  @override
  final BuiltList<String>? stepsCommaOmitempty;
  @override
  final String? metadataCommaOmitempty;

  factory _$CreatePipelineRequest(
          [void Function(CreatePipelineRequestBuilder)? updates]) =>
      (CreatePipelineRequestBuilder()..update(updates))._build();

  _$CreatePipelineRequest._(
      {this.idCommaOmitempty,
      required this.name,
      this.descriptionCommaOmitempty,
      this.stepsCommaOmitempty,
      this.metadataCommaOmitempty})
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
        idCommaOmitempty == other.idCommaOmitempty &&
        name == other.name &&
        descriptionCommaOmitempty == other.descriptionCommaOmitempty &&
        stepsCommaOmitempty == other.stepsCommaOmitempty &&
        metadataCommaOmitempty == other.metadataCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, idCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, descriptionCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, stepsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, metadataCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreatePipelineRequest')
          ..add('idCommaOmitempty', idCommaOmitempty)
          ..add('name', name)
          ..add('descriptionCommaOmitempty', descriptionCommaOmitempty)
          ..add('stepsCommaOmitempty', stepsCommaOmitempty)
          ..add('metadataCommaOmitempty', metadataCommaOmitempty))
        .toString();
  }
}

class CreatePipelineRequestBuilder
    implements Builder<CreatePipelineRequest, CreatePipelineRequestBuilder> {
  _$CreatePipelineRequest? _$v;

  String? _idCommaOmitempty;
  String? get idCommaOmitempty => _$this._idCommaOmitempty;
  set idCommaOmitempty(String? idCommaOmitempty) =>
      _$this._idCommaOmitempty = idCommaOmitempty;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _descriptionCommaOmitempty;
  String? get descriptionCommaOmitempty => _$this._descriptionCommaOmitempty;
  set descriptionCommaOmitempty(String? descriptionCommaOmitempty) =>
      _$this._descriptionCommaOmitempty = descriptionCommaOmitempty;

  ListBuilder<String>? _stepsCommaOmitempty;
  ListBuilder<String> get stepsCommaOmitempty =>
      _$this._stepsCommaOmitempty ??= ListBuilder<String>();
  set stepsCommaOmitempty(ListBuilder<String>? stepsCommaOmitempty) =>
      _$this._stepsCommaOmitempty = stepsCommaOmitempty;

  String? _metadataCommaOmitempty;
  String? get metadataCommaOmitempty => _$this._metadataCommaOmitempty;
  set metadataCommaOmitempty(String? metadataCommaOmitempty) =>
      _$this._metadataCommaOmitempty = metadataCommaOmitempty;

  CreatePipelineRequestBuilder() {
    CreatePipelineRequest._defaults(this);
  }

  CreatePipelineRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _idCommaOmitempty = $v.idCommaOmitempty;
      _name = $v.name;
      _descriptionCommaOmitempty = $v.descriptionCommaOmitempty;
      _stepsCommaOmitempty = $v.stepsCommaOmitempty?.toBuilder();
      _metadataCommaOmitempty = $v.metadataCommaOmitempty;
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
            idCommaOmitempty: idCommaOmitempty,
            name: BuiltValueNullFieldError.checkNotNull(
                name, r'CreatePipelineRequest', 'name'),
            descriptionCommaOmitempty: descriptionCommaOmitempty,
            stepsCommaOmitempty: _stepsCommaOmitempty?.build(),
            metadataCommaOmitempty: metadataCommaOmitempty,
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'stepsCommaOmitempty';
        _stepsCommaOmitempty?.build();
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
