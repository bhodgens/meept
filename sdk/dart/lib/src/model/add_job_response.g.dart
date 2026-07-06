// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'add_job_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AddJobResponse extends AddJobResponse {
  @override
  final String id;
  @override
  final String name;
  @override
  final String schedule;
  @override
  final bool enabled;
  @override
  final String? lastRun;
  @override
  final String? nextRun;
  @override
  final String? lastError;
  @override
  final int runCount;
  @override
  final bool isRunning;

  factory _$AddJobResponse([void Function(AddJobResponseBuilder)? updates]) =>
      (AddJobResponseBuilder()..update(updates))._build();

  _$AddJobResponse._(
      {required this.id,
      required this.name,
      required this.schedule,
      required this.enabled,
      this.lastRun,
      this.nextRun,
      this.lastError,
      required this.runCount,
      required this.isRunning})
      : super._();
  @override
  AddJobResponse rebuild(void Function(AddJobResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AddJobResponseBuilder toBuilder() => AddJobResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AddJobResponse &&
        id == other.id &&
        name == other.name &&
        schedule == other.schedule &&
        enabled == other.enabled &&
        lastRun == other.lastRun &&
        nextRun == other.nextRun &&
        lastError == other.lastError &&
        runCount == other.runCount &&
        isRunning == other.isRunning;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, schedule.hashCode);
    _$hash = $jc(_$hash, enabled.hashCode);
    _$hash = $jc(_$hash, lastRun.hashCode);
    _$hash = $jc(_$hash, nextRun.hashCode);
    _$hash = $jc(_$hash, lastError.hashCode);
    _$hash = $jc(_$hash, runCount.hashCode);
    _$hash = $jc(_$hash, isRunning.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AddJobResponse')
          ..add('id', id)
          ..add('name', name)
          ..add('schedule', schedule)
          ..add('enabled', enabled)
          ..add('lastRun', lastRun)
          ..add('nextRun', nextRun)
          ..add('lastError', lastError)
          ..add('runCount', runCount)
          ..add('isRunning', isRunning))
        .toString();
  }
}

class AddJobResponseBuilder
    implements Builder<AddJobResponse, AddJobResponseBuilder> {
  _$AddJobResponse? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _schedule;
  String? get schedule => _$this._schedule;
  set schedule(String? schedule) => _$this._schedule = schedule;

  bool? _enabled;
  bool? get enabled => _$this._enabled;
  set enabled(bool? enabled) => _$this._enabled = enabled;

  String? _lastRun;
  String? get lastRun => _$this._lastRun;
  set lastRun(String? lastRun) =>
      _$this._lastRun = lastRun;

  String? _nextRun;
  String? get nextRun => _$this._nextRun;
  set nextRun(String? nextRun) =>
      _$this._nextRun = nextRun;

  String? _lastError;
  String? get lastError => _$this._lastError;
  set lastError(String? lastError) =>
      _$this._lastError = lastError;

  int? _runCount;
  int? get runCount => _$this._runCount;
  set runCount(int? runCount) => _$this._runCount = runCount;

  bool? _isRunning;
  bool? get isRunning => _$this._isRunning;
  set isRunning(bool? isRunning) => _$this._isRunning = isRunning;

  AddJobResponseBuilder() {
    AddJobResponse._defaults(this);
  }

  AddJobResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _schedule = $v.schedule;
      _enabled = $v.enabled;
      _lastRun = $v.lastRun;
      _nextRun = $v.nextRun;
      _lastError = $v.lastError;
      _runCount = $v.runCount;
      _isRunning = $v.isRunning;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AddJobResponse other) {
    _$v = other as _$AddJobResponse;
  }

  @override
  void update(void Function(AddJobResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AddJobResponse build() => _build();

  _$AddJobResponse _build() {
    final _$result = _$v ??
        _$AddJobResponse._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'AddJobResponse', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'AddJobResponse', 'name'),
          schedule: BuiltValueNullFieldError.checkNotNull(
              schedule, r'AddJobResponse', 'schedule'),
          enabled: BuiltValueNullFieldError.checkNotNull(
              enabled, r'AddJobResponse', 'enabled'),
          lastRun: lastRun,
          nextRun: nextRun,
          lastError: lastError,
          runCount: BuiltValueNullFieldError.checkNotNull(
              runCount, r'AddJobResponse', 'runCount'),
          isRunning: BuiltValueNullFieldError.checkNotNull(
              isRunning, r'AddJobResponse', 'isRunning'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
