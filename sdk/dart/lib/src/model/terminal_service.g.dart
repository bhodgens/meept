// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'terminal_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TerminalService extends TerminalService {
  @override
  final JsonObject? shellTool;
  @override
  final JsonObject? bus;
  @override
  final JsonObject? logger;
  @override
  final BuiltList<String>? history;
  @override
  final JsonObject? historyMu;
  @override
  final int? maxHistory;
  @override
  final String? workingDir;
  @override
  final String? sessionStore;
  @override
  final JsonObject? sessionMu;

  factory _$TerminalService([void Function(TerminalServiceBuilder)? updates]) =>
      (TerminalServiceBuilder()..update(updates))._build();

  _$TerminalService._(
      {this.shellTool,
      this.bus,
      this.logger,
      this.history,
      this.historyMu,
      this.maxHistory,
      this.workingDir,
      this.sessionStore,
      this.sessionMu})
      : super._();
  @override
  TerminalService rebuild(void Function(TerminalServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TerminalServiceBuilder toBuilder() => TerminalServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TerminalService &&
        shellTool == other.shellTool &&
        bus == other.bus &&
        logger == other.logger &&
        history == other.history &&
        historyMu == other.historyMu &&
        maxHistory == other.maxHistory &&
        workingDir == other.workingDir &&
        sessionStore == other.sessionStore &&
        sessionMu == other.sessionMu;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, shellTool.hashCode);
    _$hash = $jc(_$hash, bus.hashCode);
    _$hash = $jc(_$hash, logger.hashCode);
    _$hash = $jc(_$hash, history.hashCode);
    _$hash = $jc(_$hash, historyMu.hashCode);
    _$hash = $jc(_$hash, maxHistory.hashCode);
    _$hash = $jc(_$hash, workingDir.hashCode);
    _$hash = $jc(_$hash, sessionStore.hashCode);
    _$hash = $jc(_$hash, sessionMu.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TerminalService')
          ..add('shellTool', shellTool)
          ..add('bus', bus)
          ..add('logger', logger)
          ..add('history', history)
          ..add('historyMu', historyMu)
          ..add('maxHistory', maxHistory)
          ..add('workingDir', workingDir)
          ..add('sessionStore', sessionStore)
          ..add('sessionMu', sessionMu))
        .toString();
  }
}

class TerminalServiceBuilder
    implements Builder<TerminalService, TerminalServiceBuilder> {
  _$TerminalService? _$v;

  JsonObject? _shellTool;
  JsonObject? get shellTool => _$this._shellTool;
  set shellTool(JsonObject? shellTool) => _$this._shellTool = shellTool;

  JsonObject? _bus;
  JsonObject? get bus => _$this._bus;
  set bus(JsonObject? bus) => _$this._bus = bus;

  JsonObject? _logger;
  JsonObject? get logger => _$this._logger;
  set logger(JsonObject? logger) => _$this._logger = logger;

  ListBuilder<String>? _history;
  ListBuilder<String> get history => _$this._history ??= ListBuilder<String>();
  set history(ListBuilder<String>? history) => _$this._history = history;

  JsonObject? _historyMu;
  JsonObject? get historyMu => _$this._historyMu;
  set historyMu(JsonObject? historyMu) => _$this._historyMu = historyMu;

  int? _maxHistory;
  int? get maxHistory => _$this._maxHistory;
  set maxHistory(int? maxHistory) => _$this._maxHistory = maxHistory;

  String? _workingDir;
  String? get workingDir => _$this._workingDir;
  set workingDir(String? workingDir) => _$this._workingDir = workingDir;

  String? _sessionStore;
  String? get sessionStore => _$this._sessionStore;
  set sessionStore(String? sessionStore) => _$this._sessionStore = sessionStore;

  JsonObject? _sessionMu;
  JsonObject? get sessionMu => _$this._sessionMu;
  set sessionMu(JsonObject? sessionMu) => _$this._sessionMu = sessionMu;

  TerminalServiceBuilder() {
    TerminalService._defaults(this);
  }

  TerminalServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _shellTool = $v.shellTool;
      _bus = $v.bus;
      _logger = $v.logger;
      _history = $v.history?.toBuilder();
      _historyMu = $v.historyMu;
      _maxHistory = $v.maxHistory;
      _workingDir = $v.workingDir;
      _sessionStore = $v.sessionStore;
      _sessionMu = $v.sessionMu;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TerminalService other) {
    _$v = other as _$TerminalService;
  }

  @override
  void update(void Function(TerminalServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TerminalService build() => _build();

  _$TerminalService _build() {
    _$TerminalService _$result;
    try {
      _$result = _$v ??
          _$TerminalService._(
            shellTool: shellTool,
            bus: bus,
            logger: logger,
            history: _history?.build(),
            historyMu: historyMu,
            maxHistory: maxHistory,
            workingDir: workingDir,
            sessionStore: sessionStore,
            sessionMu: sessionMu,
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'history';
        _history?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'TerminalService', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
