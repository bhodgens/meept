// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'shell_job_config.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ShellJobConfig extends ShellJobConfig {
  @override
  final String command;
  @override
  final String? args;
  @override
  final String? workDir;
  @override
  final String? env;
  @override
  final int? timeoutSecs;
  @override
  final bool captureOutput;

  factory _$ShellJobConfig([void Function(ShellJobConfigBuilder)? updates]) =>
      (ShellJobConfigBuilder()..update(updates))._build();

  _$ShellJobConfig._(
      {required this.command,
      this.args,
      this.workDir,
      this.env,
      this.timeoutSecs,
      required this.captureOutput})
      : super._();
  @override
  ShellJobConfig rebuild(void Function(ShellJobConfigBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ShellJobConfigBuilder toBuilder() => ShellJobConfigBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ShellJobConfig &&
        command == other.command &&
        args == other.args &&
        workDir == other.workDir &&
        env == other.env &&
        timeoutSecs == other.timeoutSecs &&
        captureOutput == other.captureOutput;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, command.hashCode);
    _$hash = $jc(_$hash, args.hashCode);
    _$hash = $jc(_$hash, workDir.hashCode);
    _$hash = $jc(_$hash, env.hashCode);
    _$hash = $jc(_$hash, timeoutSecs.hashCode);
    _$hash = $jc(_$hash, captureOutput.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ShellJobConfig')
          ..add('command', command)
          ..add('args', args)
          ..add('workDir', workDir)
          ..add('env', env)
          ..add('timeoutSecs', timeoutSecs)
          ..add('captureOutput', captureOutput))
        .toString();
  }
}

class ShellJobConfigBuilder
    implements Builder<ShellJobConfig, ShellJobConfigBuilder> {
  _$ShellJobConfig? _$v;

  String? _command;
  String? get command => _$this._command;
  set command(String? command) => _$this._command = command;

  String? _args;
  String? get args => _$this._args;
  set args(String? args) =>
      _$this._args = args;

  String? _workDir;
  String? get workDir => _$this._workDir;
  set workDir(String? workDir) =>
      _$this._workDir = workDir;

  String? _env;
  String? get env => _$this._env;
  set env(String? env) =>
      _$this._env = env;

  int? _timeoutSecs;
  int? get timeoutSecs => _$this._timeoutSecs;
  set timeoutSecs(int? timeoutSecs) =>
      _$this._timeoutSecs = timeoutSecs;

  bool? _captureOutput;
  bool? get captureOutput => _$this._captureOutput;
  set captureOutput(bool? captureOutput) =>
      _$this._captureOutput = captureOutput;

  ShellJobConfigBuilder() {
    ShellJobConfig._defaults(this);
  }

  ShellJobConfigBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _command = $v.command;
      _args = $v.args;
      _workDir = $v.workDir;
      _env = $v.env;
      _timeoutSecs = $v.timeoutSecs;
      _captureOutput = $v.captureOutput;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ShellJobConfig other) {
    _$v = other as _$ShellJobConfig;
  }

  @override
  void update(void Function(ShellJobConfigBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ShellJobConfig build() => _build();

  _$ShellJobConfig _build() {
    final _$result = _$v ??
        _$ShellJobConfig._(
          command: BuiltValueNullFieldError.checkNotNull(
              command, r'ShellJobConfig', 'command'),
          args: args,
          workDir: workDir,
          env: env,
          timeoutSecs: timeoutSecs,
          captureOutput: BuiltValueNullFieldError.checkNotNull(
              captureOutput, r'ShellJobConfig', 'captureOutput'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
