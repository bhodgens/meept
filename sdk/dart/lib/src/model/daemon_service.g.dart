// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'daemon_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$DaemonService extends DaemonService {
  @override
  final String? pidFile;
  @override
  final String? stateDir;
  @override
  final String? binPath;
  @override
  final JsonObject? controller;

  factory _$DaemonService([void Function(DaemonServiceBuilder)? updates]) =>
      (DaemonServiceBuilder()..update(updates))._build();

  _$DaemonService._(
      {this.pidFile, this.stateDir, this.binPath, this.controller})
      : super._();
  @override
  DaemonService rebuild(void Function(DaemonServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  DaemonServiceBuilder toBuilder() => DaemonServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is DaemonService &&
        pidFile == other.pidFile &&
        stateDir == other.stateDir &&
        binPath == other.binPath &&
        controller == other.controller;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pidFile.hashCode);
    _$hash = $jc(_$hash, stateDir.hashCode);
    _$hash = $jc(_$hash, binPath.hashCode);
    _$hash = $jc(_$hash, controller.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'DaemonService')
          ..add('pidFile', pidFile)
          ..add('stateDir', stateDir)
          ..add('binPath', binPath)
          ..add('controller', controller))
        .toString();
  }
}

class DaemonServiceBuilder
    implements Builder<DaemonService, DaemonServiceBuilder> {
  _$DaemonService? _$v;

  String? _pidFile;
  String? get pidFile => _$this._pidFile;
  set pidFile(String? pidFile) => _$this._pidFile = pidFile;

  String? _stateDir;
  String? get stateDir => _$this._stateDir;
  set stateDir(String? stateDir) => _$this._stateDir = stateDir;

  String? _binPath;
  String? get binPath => _$this._binPath;
  set binPath(String? binPath) => _$this._binPath = binPath;

  JsonObject? _controller;
  JsonObject? get controller => _$this._controller;
  set controller(JsonObject? controller) => _$this._controller = controller;

  DaemonServiceBuilder() {
    DaemonService._defaults(this);
  }

  DaemonServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pidFile = $v.pidFile;
      _stateDir = $v.stateDir;
      _binPath = $v.binPath;
      _controller = $v.controller;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(DaemonService other) {
    _$v = other as _$DaemonService;
  }

  @override
  void update(void Function(DaemonServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  DaemonService build() => _build();

  _$DaemonService _build() {
    final _$result = _$v ??
        _$DaemonService._(
          pidFile: pidFile,
          stateDir: stateDir,
          binPath: binPath,
          controller: controller,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
