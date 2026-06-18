// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'project_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ProjectService extends ProjectService {
  @override
  final JsonObject? pm;
  @override
  final JsonObject? store;

  factory _$ProjectService([void Function(ProjectServiceBuilder)? updates]) =>
      (ProjectServiceBuilder()..update(updates))._build();

  _$ProjectService._({this.pm, this.store}) : super._();
  @override
  ProjectService rebuild(void Function(ProjectServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ProjectServiceBuilder toBuilder() => ProjectServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ProjectService && pm == other.pm && store == other.store;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pm.hashCode);
    _$hash = $jc(_$hash, store.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ProjectService')
          ..add('pm', pm)
          ..add('store', store))
        .toString();
  }
}

class ProjectServiceBuilder
    implements Builder<ProjectService, ProjectServiceBuilder> {
  _$ProjectService? _$v;

  JsonObject? _pm;
  JsonObject? get pm => _$this._pm;
  set pm(JsonObject? pm) => _$this._pm = pm;

  JsonObject? _store;
  JsonObject? get store => _$this._store;
  set store(JsonObject? store) => _$this._store = store;

  ProjectServiceBuilder() {
    ProjectService._defaults(this);
  }

  ProjectServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pm = $v.pm;
      _store = $v.store;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ProjectService other) {
    _$v = other as _$ProjectService;
  }

  @override
  void update(void Function(ProjectServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ProjectService build() => _build();

  _$ProjectService _build() {
    final _$result = _$v ??
        _$ProjectService._(
          pm: pm,
          store: store,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
