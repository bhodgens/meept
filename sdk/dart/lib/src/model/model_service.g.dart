// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'model_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ModelService extends ModelService {
  @override
  final String? configPath;
  @override
  final JsonObject? credStore;
  @override
  final String? stateDir;

  factory _$ModelService([void Function(ModelServiceBuilder)? updates]) =>
      (ModelServiceBuilder()..update(updates))._build();

  _$ModelService._({this.configPath, this.credStore, this.stateDir})
      : super._();
  @override
  ModelService rebuild(void Function(ModelServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ModelServiceBuilder toBuilder() => ModelServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ModelService &&
        configPath == other.configPath &&
        credStore == other.credStore &&
        stateDir == other.stateDir;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, configPath.hashCode);
    _$hash = $jc(_$hash, credStore.hashCode);
    _$hash = $jc(_$hash, stateDir.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ModelService')
          ..add('configPath', configPath)
          ..add('credStore', credStore)
          ..add('stateDir', stateDir))
        .toString();
  }
}

class ModelServiceBuilder
    implements Builder<ModelService, ModelServiceBuilder> {
  _$ModelService? _$v;

  String? _configPath;
  String? get configPath => _$this._configPath;
  set configPath(String? configPath) => _$this._configPath = configPath;

  JsonObject? _credStore;
  JsonObject? get credStore => _$this._credStore;
  set credStore(JsonObject? credStore) => _$this._credStore = credStore;

  String? _stateDir;
  String? get stateDir => _$this._stateDir;
  set stateDir(String? stateDir) => _$this._stateDir = stateDir;

  ModelServiceBuilder() {
    ModelService._defaults(this);
  }

  ModelServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _configPath = $v.configPath;
      _credStore = $v.credStore;
      _stateDir = $v.stateDir;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ModelService other) {
    _$v = other as _$ModelService;
  }

  @override
  void update(void Function(ModelServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ModelService build() => _build();

  _$ModelService _build() {
    final _$result = _$v ??
        _$ModelService._(
          configPath: configPath,
          credStore: credStore,
          stateDir: stateDir,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
