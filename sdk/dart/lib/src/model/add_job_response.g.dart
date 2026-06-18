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
  final String? lastRunCommaOmitempty;
  @override
  final String? nextRunCommaOmitempty;
  @override
  final String? lastErrorCommaOmitempty;
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
      this.lastRunCommaOmitempty,
      this.nextRunCommaOmitempty,
      this.lastErrorCommaOmitempty,
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
    return (newBuiltValueToStringHelper(r'AddJobResponse')
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
          lastRunCommaOmitempty: lastRunCommaOmitempty,
          nextRunCommaOmitempty: nextRunCommaOmitempty,
          lastErrorCommaOmitempty: lastErrorCommaOmitempty,
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
