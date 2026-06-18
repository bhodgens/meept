// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'worker_stats_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$WorkerStatsResponse extends WorkerStatsResponse {
  @override
  final int totalWorkers;
  @override
  final int idleWorkers;
  @override
  final int busyWorkers;
  @override
  final int errorWorkers;
  @override
  final BuiltList<String>? workerStats;

  factory _$WorkerStatsResponse(
          [void Function(WorkerStatsResponseBuilder)? updates]) =>
      (WorkerStatsResponseBuilder()..update(updates))._build();

  _$WorkerStatsResponse._(
      {required this.totalWorkers,
      required this.idleWorkers,
      required this.busyWorkers,
      required this.errorWorkers,
      this.workerStats})
      : super._();
  @override
  WorkerStatsResponse rebuild(
          void Function(WorkerStatsResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  WorkerStatsResponseBuilder toBuilder() =>
      WorkerStatsResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is WorkerStatsResponse &&
        totalWorkers == other.totalWorkers &&
        idleWorkers == other.idleWorkers &&
        busyWorkers == other.busyWorkers &&
        errorWorkers == other.errorWorkers &&
        workerStats == other.workerStats;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, totalWorkers.hashCode);
    _$hash = $jc(_$hash, idleWorkers.hashCode);
    _$hash = $jc(_$hash, busyWorkers.hashCode);
    _$hash = $jc(_$hash, errorWorkers.hashCode);
    _$hash = $jc(_$hash, workerStats.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'WorkerStatsResponse')
          ..add('totalWorkers', totalWorkers)
          ..add('idleWorkers', idleWorkers)
          ..add('busyWorkers', busyWorkers)
          ..add('errorWorkers', errorWorkers)
          ..add('workerStats', workerStats))
        .toString();
  }
}

class WorkerStatsResponseBuilder
    implements Builder<WorkerStatsResponse, WorkerStatsResponseBuilder> {
  _$WorkerStatsResponse? _$v;

  int? _totalWorkers;
  int? get totalWorkers => _$this._totalWorkers;
  set totalWorkers(int? totalWorkers) => _$this._totalWorkers = totalWorkers;

  int? _idleWorkers;
  int? get idleWorkers => _$this._idleWorkers;
  set idleWorkers(int? idleWorkers) => _$this._idleWorkers = idleWorkers;

  int? _busyWorkers;
  int? get busyWorkers => _$this._busyWorkers;
  set busyWorkers(int? busyWorkers) => _$this._busyWorkers = busyWorkers;

  int? _errorWorkers;
  int? get errorWorkers => _$this._errorWorkers;
  set errorWorkers(int? errorWorkers) => _$this._errorWorkers = errorWorkers;

  ListBuilder<String>? _workerStats;
  ListBuilder<String> get workerStats =>
      _$this._workerStats ??= ListBuilder<String>();
  set workerStats(ListBuilder<String>? workerStats) =>
      _$this._workerStats = workerStats;

  WorkerStatsResponseBuilder() {
    WorkerStatsResponse._defaults(this);
  }

  WorkerStatsResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _totalWorkers = $v.totalWorkers;
      _idleWorkers = $v.idleWorkers;
      _busyWorkers = $v.busyWorkers;
      _errorWorkers = $v.errorWorkers;
      _workerStats = $v.workerStats?.toBuilder();
      _$v = null;
    }
    return this;
  }

  @override
  void replace(WorkerStatsResponse other) {
    _$v = other as _$WorkerStatsResponse;
  }

  @override
  void update(void Function(WorkerStatsResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  WorkerStatsResponse build() => _build();

  _$WorkerStatsResponse _build() {
    _$WorkerStatsResponse _$result;
    try {
      _$result = _$v ??
          _$WorkerStatsResponse._(
            totalWorkers: BuiltValueNullFieldError.checkNotNull(
                totalWorkers, r'WorkerStatsResponse', 'totalWorkers'),
            idleWorkers: BuiltValueNullFieldError.checkNotNull(
                idleWorkers, r'WorkerStatsResponse', 'idleWorkers'),
            busyWorkers: BuiltValueNullFieldError.checkNotNull(
                busyWorkers, r'WorkerStatsResponse', 'busyWorkers'),
            errorWorkers: BuiltValueNullFieldError.checkNotNull(
                errorWorkers, r'WorkerStatsResponse', 'errorWorkers'),
            workerStats: _workerStats?.build(),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'workerStats';
        _workerStats?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'WorkerStatsResponse', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
