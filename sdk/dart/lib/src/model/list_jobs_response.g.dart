// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_jobs_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListJobsResponse extends ListJobsResponse {
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

  factory _$ListJobsResponse(
          [void Function(ListJobsResponseBuilder)? updates]) =>
      (ListJobsResponseBuilder()..update(updates))._build();

  _$ListJobsResponse._(
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
  ListJobsResponse rebuild(void Function(ListJobsResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListJobsResponseBuilder toBuilder() =>
      ListJobsResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListJobsResponse &&
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
    return (newBuiltValueToStringHelper(r'ListJobsResponse')
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

class ListJobsResponseBuilder
    implements Builder<ListJobsResponse, ListJobsResponseBuilder> {
  _$ListJobsResponse? _$v;

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

  ListJobsResponseBuilder() {
    ListJobsResponse._defaults(this);
  }

  ListJobsResponseBuilder get _$this {
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
  void replace(ListJobsResponse other) {
    _$v = other as _$ListJobsResponse;
  }

  @override
  void update(void Function(ListJobsResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListJobsResponse build() => _build();

  _$ListJobsResponse _build() {
    final _$result = _$v ??
        _$ListJobsResponse._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'ListJobsResponse', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'ListJobsResponse', 'name'),
          schedule: BuiltValueNullFieldError.checkNotNull(
              schedule, r'ListJobsResponse', 'schedule'),
          enabled: BuiltValueNullFieldError.checkNotNull(
              enabled, r'ListJobsResponse', 'enabled'),
          lastRun: lastRun,
          nextRun: nextRun,
          lastError: lastError,
          runCount: BuiltValueNullFieldError.checkNotNull(
              runCount, r'ListJobsResponse', 'runCount'),
          isRunning: BuiltValueNullFieldError.checkNotNull(
              isRunning, r'ListJobsResponse', 'isRunning'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
