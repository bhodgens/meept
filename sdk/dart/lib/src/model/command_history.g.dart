// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'command_history.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CommandHistory extends CommandHistory {
  @override
  final String id;
  @override
  final String command;
  @override
  final String? outputCommaOmitempty;
  @override
  final String? stderrCommaOmitempty;
  @override
  final int exitCode;
  @override
  final String timestamp;
  @override
  final String workingDir;
  @override
  final JsonObject durationMs;
  @override
  final JsonObject riskLevel;
  @override
  final bool success;

  factory _$CommandHistory([void Function(CommandHistoryBuilder)? updates]) =>
      (CommandHistoryBuilder()..update(updates))._build();

  _$CommandHistory._(
      {required this.id,
      required this.command,
      this.outputCommaOmitempty,
      this.stderrCommaOmitempty,
      required this.exitCode,
      required this.timestamp,
      required this.workingDir,
      required this.durationMs,
      required this.riskLevel,
      required this.success})
      : super._();
  @override
  CommandHistory rebuild(void Function(CommandHistoryBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CommandHistoryBuilder toBuilder() => CommandHistoryBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CommandHistory &&
        id == other.id &&
        command == other.command &&
        outputCommaOmitempty == other.outputCommaOmitempty &&
        stderrCommaOmitempty == other.stderrCommaOmitempty &&
        exitCode == other.exitCode &&
        timestamp == other.timestamp &&
        workingDir == other.workingDir &&
        durationMs == other.durationMs &&
        riskLevel == other.riskLevel &&
        success == other.success;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, command.hashCode);
    _$hash = $jc(_$hash, outputCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, stderrCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, exitCode.hashCode);
    _$hash = $jc(_$hash, timestamp.hashCode);
    _$hash = $jc(_$hash, workingDir.hashCode);
    _$hash = $jc(_$hash, durationMs.hashCode);
    _$hash = $jc(_$hash, riskLevel.hashCode);
    _$hash = $jc(_$hash, success.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CommandHistory')
          ..add('id', id)
          ..add('command', command)
          ..add('outputCommaOmitempty', outputCommaOmitempty)
          ..add('stderrCommaOmitempty', stderrCommaOmitempty)
          ..add('exitCode', exitCode)
          ..add('timestamp', timestamp)
          ..add('workingDir', workingDir)
          ..add('durationMs', durationMs)
          ..add('riskLevel', riskLevel)
          ..add('success', success))
        .toString();
  }
}

class CommandHistoryBuilder
    implements Builder<CommandHistory, CommandHistoryBuilder> {
  _$CommandHistory? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _command;
  String? get command => _$this._command;
  set command(String? command) => _$this._command = command;

  String? _outputCommaOmitempty;
  String? get outputCommaOmitempty => _$this._outputCommaOmitempty;
  set outputCommaOmitempty(String? outputCommaOmitempty) =>
      _$this._outputCommaOmitempty = outputCommaOmitempty;

  String? _stderrCommaOmitempty;
  String? get stderrCommaOmitempty => _$this._stderrCommaOmitempty;
  set stderrCommaOmitempty(String? stderrCommaOmitempty) =>
      _$this._stderrCommaOmitempty = stderrCommaOmitempty;

  int? _exitCode;
  int? get exitCode => _$this._exitCode;
  set exitCode(int? exitCode) => _$this._exitCode = exitCode;

  String? _timestamp;
  String? get timestamp => _$this._timestamp;
  set timestamp(String? timestamp) => _$this._timestamp = timestamp;

  String? _workingDir;
  String? get workingDir => _$this._workingDir;
  set workingDir(String? workingDir) => _$this._workingDir = workingDir;

  JsonObject? _durationMs;
  JsonObject? get durationMs => _$this._durationMs;
  set durationMs(JsonObject? durationMs) => _$this._durationMs = durationMs;

  JsonObject? _riskLevel;
  JsonObject? get riskLevel => _$this._riskLevel;
  set riskLevel(JsonObject? riskLevel) => _$this._riskLevel = riskLevel;

  bool? _success;
  bool? get success => _$this._success;
  set success(bool? success) => _$this._success = success;

  CommandHistoryBuilder() {
    CommandHistory._defaults(this);
  }

  CommandHistoryBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _command = $v.command;
      _outputCommaOmitempty = $v.outputCommaOmitempty;
      _stderrCommaOmitempty = $v.stderrCommaOmitempty;
      _exitCode = $v.exitCode;
      _timestamp = $v.timestamp;
      _workingDir = $v.workingDir;
      _durationMs = $v.durationMs;
      _riskLevel = $v.riskLevel;
      _success = $v.success;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CommandHistory other) {
    _$v = other as _$CommandHistory;
  }

  @override
  void update(void Function(CommandHistoryBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CommandHistory build() => _build();

  _$CommandHistory _build() {
    final _$result = _$v ??
        _$CommandHistory._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'CommandHistory', 'id'),
          command: BuiltValueNullFieldError.checkNotNull(
              command, r'CommandHistory', 'command'),
          outputCommaOmitempty: outputCommaOmitempty,
          stderrCommaOmitempty: stderrCommaOmitempty,
          exitCode: BuiltValueNullFieldError.checkNotNull(
              exitCode, r'CommandHistory', 'exitCode'),
          timestamp: BuiltValueNullFieldError.checkNotNull(
              timestamp, r'CommandHistory', 'timestamp'),
          workingDir: BuiltValueNullFieldError.checkNotNull(
              workingDir, r'CommandHistory', 'workingDir'),
          durationMs: BuiltValueNullFieldError.checkNotNull(
              durationMs, r'CommandHistory', 'durationMs'),
          riskLevel: BuiltValueNullFieldError.checkNotNull(
              riskLevel, r'CommandHistory', 'riskLevel'),
          success: BuiltValueNullFieldError.checkNotNull(
              success, r'CommandHistory', 'success'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
