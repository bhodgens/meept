// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'add_job_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AddJobRequest extends AddJobRequest {
  @override
  final String id;
  @override
  final String name;
  @override
  final String schedule;
  @override
  final String type;
  @override
  final JsonObject? agentConfigCommaOmitempty;
  @override
  final JsonObject? shellConfigCommaOmitempty;
  @override
  final bool? enabledCommaOmitempty;

  factory _$AddJobRequest([void Function(AddJobRequestBuilder)? updates]) =>
      (AddJobRequestBuilder()..update(updates))._build();

  _$AddJobRequest._(
      {required this.id,
      required this.name,
      required this.schedule,
      required this.type,
      this.agentConfigCommaOmitempty,
      this.shellConfigCommaOmitempty,
      this.enabledCommaOmitempty})
      : super._();
  @override
  AddJobRequest rebuild(void Function(AddJobRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AddJobRequestBuilder toBuilder() => AddJobRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AddJobRequest &&
        id == other.id &&
        name == other.name &&
        schedule == other.schedule &&
        type == other.type &&
        agentConfigCommaOmitempty == other.agentConfigCommaOmitempty &&
        shellConfigCommaOmitempty == other.shellConfigCommaOmitempty &&
        enabledCommaOmitempty == other.enabledCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, schedule.hashCode);
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, agentConfigCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, shellConfigCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, enabledCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AddJobRequest')
          ..add('id', id)
          ..add('name', name)
          ..add('schedule', schedule)
          ..add('type', type)
          ..add('agentConfigCommaOmitempty', agentConfigCommaOmitempty)
          ..add('shellConfigCommaOmitempty', shellConfigCommaOmitempty)
          ..add('enabledCommaOmitempty', enabledCommaOmitempty))
        .toString();
  }
}

class AddJobRequestBuilder
    implements Builder<AddJobRequest, AddJobRequestBuilder> {
  _$AddJobRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _schedule;
  String? get schedule => _$this._schedule;
  set schedule(String? schedule) => _$this._schedule = schedule;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  JsonObject? _agentConfigCommaOmitempty;
  JsonObject? get agentConfigCommaOmitempty =>
      _$this._agentConfigCommaOmitempty;
  set agentConfigCommaOmitempty(JsonObject? agentConfigCommaOmitempty) =>
      _$this._agentConfigCommaOmitempty = agentConfigCommaOmitempty;

  JsonObject? _shellConfigCommaOmitempty;
  JsonObject? get shellConfigCommaOmitempty =>
      _$this._shellConfigCommaOmitempty;
  set shellConfigCommaOmitempty(JsonObject? shellConfigCommaOmitempty) =>
      _$this._shellConfigCommaOmitempty = shellConfigCommaOmitempty;

  bool? _enabledCommaOmitempty;
  bool? get enabledCommaOmitempty => _$this._enabledCommaOmitempty;
  set enabledCommaOmitempty(bool? enabledCommaOmitempty) =>
      _$this._enabledCommaOmitempty = enabledCommaOmitempty;

  AddJobRequestBuilder() {
    AddJobRequest._defaults(this);
  }

  AddJobRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _schedule = $v.schedule;
      _type = $v.type;
      _agentConfigCommaOmitempty = $v.agentConfigCommaOmitempty;
      _shellConfigCommaOmitempty = $v.shellConfigCommaOmitempty;
      _enabledCommaOmitempty = $v.enabledCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AddJobRequest other) {
    _$v = other as _$AddJobRequest;
  }

  @override
  void update(void Function(AddJobRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AddJobRequest build() => _build();

  _$AddJobRequest _build() {
    final _$result = _$v ??
        _$AddJobRequest._(
          id: BuiltValueNullFieldError.checkNotNull(id, r'AddJobRequest', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'AddJobRequest', 'name'),
          schedule: BuiltValueNullFieldError.checkNotNull(
              schedule, r'AddJobRequest', 'schedule'),
          type: BuiltValueNullFieldError.checkNotNull(
              type, r'AddJobRequest', 'type'),
          agentConfigCommaOmitempty: agentConfigCommaOmitempty,
          shellConfigCommaOmitempty: shellConfigCommaOmitempty,
          enabledCommaOmitempty: enabledCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
