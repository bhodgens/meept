// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'status_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$StatusResponse extends StatusResponse {
  @override
  final bool enabled;
  @override
  final String? lastCycleCommaOmitempty;
  @override
  final int skillsLearned;
  @override
  final int pendingTasks;

  factory _$StatusResponse([void Function(StatusResponseBuilder)? updates]) =>
      (StatusResponseBuilder()..update(updates))._build();

  _$StatusResponse._(
      {required this.enabled,
      this.lastCycleCommaOmitempty,
      required this.skillsLearned,
      required this.pendingTasks})
      : super._();
  @override
  StatusResponse rebuild(void Function(StatusResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  StatusResponseBuilder toBuilder() => StatusResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is StatusResponse &&
        enabled == other.enabled &&
        lastCycleCommaOmitempty == other.lastCycleCommaOmitempty &&
        skillsLearned == other.skillsLearned &&
        pendingTasks == other.pendingTasks;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, enabled.hashCode);
    _$hash = $jc(_$hash, lastCycleCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, skillsLearned.hashCode);
    _$hash = $jc(_$hash, pendingTasks.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'StatusResponse')
          ..add('enabled', enabled)
          ..add('lastCycleCommaOmitempty', lastCycleCommaOmitempty)
          ..add('skillsLearned', skillsLearned)
          ..add('pendingTasks', pendingTasks))
        .toString();
  }
}

class StatusResponseBuilder
    implements Builder<StatusResponse, StatusResponseBuilder> {
  _$StatusResponse? _$v;

  bool? _enabled;
  bool? get enabled => _$this._enabled;
  set enabled(bool? enabled) => _$this._enabled = enabled;

  String? _lastCycleCommaOmitempty;
  String? get lastCycleCommaOmitempty => _$this._lastCycleCommaOmitempty;
  set lastCycleCommaOmitempty(String? lastCycleCommaOmitempty) =>
      _$this._lastCycleCommaOmitempty = lastCycleCommaOmitempty;

  int? _skillsLearned;
  int? get skillsLearned => _$this._skillsLearned;
  set skillsLearned(int? skillsLearned) =>
      _$this._skillsLearned = skillsLearned;

  int? _pendingTasks;
  int? get pendingTasks => _$this._pendingTasks;
  set pendingTasks(int? pendingTasks) => _$this._pendingTasks = pendingTasks;

  StatusResponseBuilder() {
    StatusResponse._defaults(this);
  }

  StatusResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _enabled = $v.enabled;
      _lastCycleCommaOmitempty = $v.lastCycleCommaOmitempty;
      _skillsLearned = $v.skillsLearned;
      _pendingTasks = $v.pendingTasks;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(StatusResponse other) {
    _$v = other as _$StatusResponse;
  }

  @override
  void update(void Function(StatusResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  StatusResponse build() => _build();

  _$StatusResponse _build() {
    final _$result = _$v ??
        _$StatusResponse._(
          enabled: BuiltValueNullFieldError.checkNotNull(
              enabled, r'StatusResponse', 'enabled'),
          lastCycleCommaOmitempty: lastCycleCommaOmitempty,
          skillsLearned: BuiltValueNullFieldError.checkNotNull(
              skillsLearned, r'StatusResponse', 'skillsLearned'),
          pendingTasks: BuiltValueNullFieldError.checkNotNull(
              pendingTasks, r'StatusResponse', 'pendingTasks'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
