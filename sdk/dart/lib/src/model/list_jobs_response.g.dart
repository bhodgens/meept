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
  final String? lastRunCommaOmitempty;
  @override
  final String? nextRunCommaOmitempty;
  @override
  final String? lastErrorCommaOmitempty;
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
      this.lastRunCommaOmitempty,
      this.nextRunCommaOmitempty,
      this.lastErrorCommaOmitempty,
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
        lastRunCommaOmitempty == other.lastRunCommaOmitempty &&
        nextRunCommaOmitempty == other.nextRunCommaOmitempty &&
        lastErrorCommaOmitempty == other.lastErrorCommaOmitempty &&
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
    _$hash = $jc(_$hash, lastRunCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, nextRunCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, lastErrorCommaOmitempty.hashCode);
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
          ..add('lastRunCommaOmitempty', lastRunCommaOmitempty)
          ..add('nextRunCommaOmitempty', nextRunCommaOmitempty)
          ..add('lastErrorCommaOmitempty', lastErrorCommaOmitempty)
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

  String? _lastRunCommaOmitempty;
  String? get lastRunCommaOmitempty => _$this._lastRunCommaOmitempty;
  set lastRunCommaOmitempty(String? lastRunCommaOmitempty) =>
      _$this._lastRunCommaOmitempty = lastRunCommaOmitempty;

  String? _nextRunCommaOmitempty;
  String? get nextRunCommaOmitempty => _$this._nextRunCommaOmitempty;
  set nextRunCommaOmitempty(String? nextRunCommaOmitempty) =>
      _$this._nextRunCommaOmitempty = nextRunCommaOmitempty;

  String? _lastErrorCommaOmitempty;
  String? get lastErrorCommaOmitempty => _$this._lastErrorCommaOmitempty;
  set lastErrorCommaOmitempty(String? lastErrorCommaOmitempty) =>
      _$this._lastErrorCommaOmitempty = lastErrorCommaOmitempty;

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
      _lastRunCommaOmitempty = $v.lastRunCommaOmitempty;
      _nextRunCommaOmitempty = $v.nextRunCommaOmitempty;
      _lastErrorCommaOmitempty = $v.lastErrorCommaOmitempty;
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
          lastRunCommaOmitempty: lastRunCommaOmitempty,
          nextRunCommaOmitempty: nextRunCommaOmitempty,
          lastErrorCommaOmitempty: lastErrorCommaOmitempty,
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
