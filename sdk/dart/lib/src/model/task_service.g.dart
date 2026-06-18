// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'task_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TaskService extends TaskService {
  @override
  final JsonObject? registry;

  factory _$TaskService([void Function(TaskServiceBuilder)? updates]) =>
      (TaskServiceBuilder()..update(updates))._build();

  _$TaskService._({this.registry}) : super._();
  @override
  TaskService rebuild(void Function(TaskServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TaskServiceBuilder toBuilder() => TaskServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TaskService && registry == other.registry;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, registry.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TaskService')
          ..add('registry', registry))
        .toString();
  }
}

class TaskServiceBuilder implements Builder<TaskService, TaskServiceBuilder> {
  _$TaskService? _$v;

  JsonObject? _registry;
  JsonObject? get registry => _$this._registry;
  set registry(JsonObject? registry) => _$this._registry = registry;

  TaskServiceBuilder() {
    TaskService._defaults(this);
  }

  TaskServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _registry = $v.registry;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TaskService other) {
    _$v = other as _$TaskService;
  }

  @override
  void update(void Function(TaskServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TaskService build() => _build();

  _$TaskService _build() {
    final _$result = _$v ??
        _$TaskService._(
          registry: registry,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
