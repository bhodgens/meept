// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'shell_job_config.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ShellJobConfig extends ShellJobConfig {
  @override
  final String command;
  @override
  final String? argsCommaOmitempty;
  @override
  final String? workDirCommaOmitempty;
  @override
  final String? envCommaOmitempty;
  @override
  final int? timeoutSecsCommaOmitempty;
  @override
  final bool captureOutput;

  factory _$ShellJobConfig([void Function(ShellJobConfigBuilder)? updates]) =>
      (ShellJobConfigBuilder()..update(updates))._build();

  _$ShellJobConfig._(
      {required this.command,
      this.argsCommaOmitempty,
      this.workDirCommaOmitempty,
      this.envCommaOmitempty,
      this.timeoutSecsCommaOmitempty,
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
        argsCommaOmitempty == other.argsCommaOmitempty &&
        workDirCommaOmitempty == other.workDirCommaOmitempty &&
        envCommaOmitempty == other.envCommaOmitempty &&
        timeoutSecsCommaOmitempty == other.timeoutSecsCommaOmitempty &&
        captureOutput == other.captureOutput;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, command.hashCode);
    _$hash = $jc(_$hash, argsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, workDirCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, envCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, timeoutSecsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, captureOutput.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ShellJobConfig')
          ..add('command', command)
          ..add('argsCommaOmitempty', argsCommaOmitempty)
          ..add('workDirCommaOmitempty', workDirCommaOmitempty)
          ..add('envCommaOmitempty', envCommaOmitempty)
          ..add('timeoutSecsCommaOmitempty', timeoutSecsCommaOmitempty)
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

  String? _argsCommaOmitempty;
  String? get argsCommaOmitempty => _$this._argsCommaOmitempty;
  set argsCommaOmitempty(String? argsCommaOmitempty) =>
      _$this._argsCommaOmitempty = argsCommaOmitempty;

  String? _workDirCommaOmitempty;
  String? get workDirCommaOmitempty => _$this._workDirCommaOmitempty;
  set workDirCommaOmitempty(String? workDirCommaOmitempty) =>
      _$this._workDirCommaOmitempty = workDirCommaOmitempty;

  String? _envCommaOmitempty;
  String? get envCommaOmitempty => _$this._envCommaOmitempty;
  set envCommaOmitempty(String? envCommaOmitempty) =>
      _$this._envCommaOmitempty = envCommaOmitempty;

  int? _timeoutSecsCommaOmitempty;
  int? get timeoutSecsCommaOmitempty => _$this._timeoutSecsCommaOmitempty;
  set timeoutSecsCommaOmitempty(int? timeoutSecsCommaOmitempty) =>
      _$this._timeoutSecsCommaOmitempty = timeoutSecsCommaOmitempty;

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
      _argsCommaOmitempty = $v.argsCommaOmitempty;
      _workDirCommaOmitempty = $v.workDirCommaOmitempty;
      _envCommaOmitempty = $v.envCommaOmitempty;
      _timeoutSecsCommaOmitempty = $v.timeoutSecsCommaOmitempty;
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
          argsCommaOmitempty: argsCommaOmitempty,
          workDirCommaOmitempty: workDirCommaOmitempty,
          envCommaOmitempty: envCommaOmitempty,
          timeoutSecsCommaOmitempty: timeoutSecsCommaOmitempty,
          captureOutput: BuiltValueNullFieldError.checkNotNull(
              captureOutput, r'ShellJobConfig', 'captureOutput'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
