// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'terminal_session.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TerminalSession extends TerminalSession {
  @override
  final String? ID;
  @override
  final String? workingDir;
  @override
  final String? createdAt;
  @override
  final String? lastUsed;
  @override
  final int? commandCount;

  factory _$TerminalSession([void Function(TerminalSessionBuilder)? updates]) =>
      (TerminalSessionBuilder()..update(updates))._build();

  _$TerminalSession._(
      {this.ID,
      this.workingDir,
      this.createdAt,
      this.lastUsed,
      this.commandCount})
      : super._();
  @override
  TerminalSession rebuild(void Function(TerminalSessionBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TerminalSessionBuilder toBuilder() => TerminalSessionBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TerminalSession &&
        ID == other.ID &&
        workingDir == other.workingDir &&
        createdAt == other.createdAt &&
        lastUsed == other.lastUsed &&
        commandCount == other.commandCount;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, ID.hashCode);
    _$hash = $jc(_$hash, workingDir.hashCode);
    _$hash = $jc(_$hash, createdAt.hashCode);
    _$hash = $jc(_$hash, lastUsed.hashCode);
    _$hash = $jc(_$hash, commandCount.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TerminalSession')
          ..add('ID', ID)
          ..add('workingDir', workingDir)
          ..add('createdAt', createdAt)
          ..add('lastUsed', lastUsed)
          ..add('commandCount', commandCount))
        .toString();
  }
}

class TerminalSessionBuilder
    implements Builder<TerminalSession, TerminalSessionBuilder> {
  _$TerminalSession? _$v;

  String? _ID;
  String? get ID => _$this._ID;
  set ID(String? ID) => _$this._ID = ID;

  String? _workingDir;
  String? get workingDir => _$this._workingDir;
  set workingDir(String? workingDir) => _$this._workingDir = workingDir;

  String? _createdAt;
  String? get createdAt => _$this._createdAt;
  set createdAt(String? createdAt) => _$this._createdAt = createdAt;

  String? _lastUsed;
  String? get lastUsed => _$this._lastUsed;
  set lastUsed(String? lastUsed) => _$this._lastUsed = lastUsed;

  int? _commandCount;
  int? get commandCount => _$this._commandCount;
  set commandCount(int? commandCount) => _$this._commandCount = commandCount;

  TerminalSessionBuilder() {
    TerminalSession._defaults(this);
  }

  TerminalSessionBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _ID = $v.ID;
      _workingDir = $v.workingDir;
      _createdAt = $v.createdAt;
      _lastUsed = $v.lastUsed;
      _commandCount = $v.commandCount;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TerminalSession other) {
    _$v = other as _$TerminalSession;
  }

  @override
  void update(void Function(TerminalSessionBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TerminalSession build() => _build();

  _$TerminalSession _build() {
    final _$result = _$v ??
        _$TerminalSession._(
          ID: ID,
          workingDir: workingDir,
          createdAt: createdAt,
          lastUsed: lastUsed,
          commandCount: commandCount,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
