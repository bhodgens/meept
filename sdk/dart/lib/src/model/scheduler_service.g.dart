// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'scheduler_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SchedulerService extends SchedulerService {
  @override
  final JsonObject? scheduler;

  factory _$SchedulerService(
          [void Function(SchedulerServiceBuilder)? updates]) =>
      (SchedulerServiceBuilder()..update(updates))._build();

  _$SchedulerService._({this.scheduler}) : super._();
  @override
  SchedulerService rebuild(void Function(SchedulerServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SchedulerServiceBuilder toBuilder() =>
      SchedulerServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SchedulerService && scheduler == other.scheduler;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, scheduler.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SchedulerService')
          ..add('scheduler', scheduler))
        .toString();
  }
}

class SchedulerServiceBuilder
    implements Builder<SchedulerService, SchedulerServiceBuilder> {
  _$SchedulerService? _$v;

  JsonObject? _scheduler;
  JsonObject? get scheduler => _$this._scheduler;
  set scheduler(JsonObject? scheduler) => _$this._scheduler = scheduler;

  SchedulerServiceBuilder() {
    SchedulerService._defaults(this);
  }

  SchedulerServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _scheduler = $v.scheduler;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SchedulerService other) {
    _$v = other as _$SchedulerService;
  }

  @override
  void update(void Function(SchedulerServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SchedulerService build() => _build();

  _$SchedulerService _build() {
    final _$result = _$v ??
        _$SchedulerService._(
          scheduler: scheduler,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
