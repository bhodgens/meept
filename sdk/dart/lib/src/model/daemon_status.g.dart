// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'daemon_status.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$DaemonStatus extends DaemonStatus {
  @override
  final String status;
  @override
  final int? pidCommaOmitempty;
  @override
  final num? uptimeSecondsCommaOmitempty;
  @override
  final String? modelCommaOmitempty;
  @override
  final int tokensUsed;
  @override
  final int tokensRemaining;
  @override
  final num budgetUsed;
  @override
  final num budgetRemaining;
  @override
  final int? hourlyUsed;
  @override
  final int? hourlyRemaining;
  @override
  final int? dailyUsed;
  @override
  final int? dailyRemaining;
  @override
  final int? rpmCurrent;
  @override
  final int? rpmLimit;
  @override
  final num? dailyCostUsed;
  @override
  final num? dailyCostLimit;
  @override
  final num? hourlyCostUsed;
  @override
  final num? hourlyCostLimit;
  @override
  final num? perTaskCost;
  @override
  final int? perTaskBudget;
  @override
  final num? perSessionCost;
  @override
  final int? perSessionBudget;
  @override
  final int registeredMethods;
  @override
  final int busSubscribers;

  factory _$DaemonStatus([void Function(DaemonStatusBuilder)? updates]) =>
      (DaemonStatusBuilder()..update(updates))._build();

  _$DaemonStatus._(
      {required this.status,
      this.pidCommaOmitempty,
      this.uptimeSecondsCommaOmitempty,
      this.modelCommaOmitempty,
      required this.tokensUsed,
      required this.tokensRemaining,
      required this.budgetUsed,
      required this.budgetRemaining,
      this.hourlyUsed,
      this.hourlyRemaining,
      this.dailyUsed,
      this.dailyRemaining,
      this.rpmCurrent,
      this.rpmLimit,
      this.dailyCostUsed,
      this.dailyCostLimit,
      this.hourlyCostUsed,
      this.hourlyCostLimit,
      this.perTaskCost,
      this.perTaskBudget,
      this.perSessionCost,
      this.perSessionBudget,
      required this.registeredMethods,
      required this.busSubscribers})
      : super._();
  @override
  DaemonStatus rebuild(void Function(DaemonStatusBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  DaemonStatusBuilder toBuilder() => DaemonStatusBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is DaemonStatus &&
        status == other.status &&
        pidCommaOmitempty == other.pidCommaOmitempty &&
        uptimeSecondsCommaOmitempty == other.uptimeSecondsCommaOmitempty &&
        modelCommaOmitempty == other.modelCommaOmitempty &&
        tokensUsed == other.tokensUsed &&
        tokensRemaining == other.tokensRemaining &&
        budgetUsed == other.budgetUsed &&
        budgetRemaining == other.budgetRemaining &&
        hourlyUsed == other.hourlyUsed &&
        hourlyRemaining == other.hourlyRemaining &&
        dailyUsed == other.dailyUsed &&
        dailyRemaining == other.dailyRemaining &&
        rpmCurrent == other.rpmCurrent &&
        rpmLimit == other.rpmLimit &&
        dailyCostUsed == other.dailyCostUsed &&
        dailyCostLimit == other.dailyCostLimit &&
        hourlyCostUsed == other.hourlyCostUsed &&
        hourlyCostLimit == other.hourlyCostLimit &&
        perTaskCost == other.perTaskCost &&
        perTaskBudget == other.perTaskBudget &&
        perSessionCost == other.perSessionCost &&
        perSessionBudget == other.perSessionBudget &&
        registeredMethods == other.registeredMethods &&
        busSubscribers == other.busSubscribers;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, pidCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, uptimeSecondsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, modelCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, tokensUsed.hashCode);
    _$hash = $jc(_$hash, tokensRemaining.hashCode);
    _$hash = $jc(_$hash, budgetUsed.hashCode);
    _$hash = $jc(_$hash, budgetRemaining.hashCode);
    _$hash = $jc(_$hash, hourlyUsed.hashCode);
    _$hash = $jc(_$hash, hourlyRemaining.hashCode);
    _$hash = $jc(_$hash, dailyUsed.hashCode);
    _$hash = $jc(_$hash, dailyRemaining.hashCode);
    _$hash = $jc(_$hash, rpmCurrent.hashCode);
    _$hash = $jc(_$hash, rpmLimit.hashCode);
    _$hash = $jc(_$hash, dailyCostUsed.hashCode);
    _$hash = $jc(_$hash, dailyCostLimit.hashCode);
    _$hash = $jc(_$hash, hourlyCostUsed.hashCode);
    _$hash = $jc(_$hash, hourlyCostLimit.hashCode);
    _$hash = $jc(_$hash, perTaskCost.hashCode);
    _$hash = $jc(_$hash, perTaskBudget.hashCode);
    _$hash = $jc(_$hash, perSessionCost.hashCode);
    _$hash = $jc(_$hash, perSessionBudget.hashCode);
    _$hash = $jc(_$hash, registeredMethods.hashCode);
    _$hash = $jc(_$hash, busSubscribers.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'DaemonStatus')
          ..add('status', status)
          ..add('pidCommaOmitempty', pidCommaOmitempty)
          ..add('uptimeSecondsCommaOmitempty', uptimeSecondsCommaOmitempty)
          ..add('modelCommaOmitempty', modelCommaOmitempty)
          ..add('tokensUsed', tokensUsed)
          ..add('tokensRemaining', tokensRemaining)
          ..add('budgetUsed', budgetUsed)
          ..add('budgetRemaining', budgetRemaining)
          ..add('hourlyUsed', hourlyUsed)
          ..add('hourlyRemaining', hourlyRemaining)
          ..add('dailyUsed', dailyUsed)
          ..add('dailyRemaining', dailyRemaining)
          ..add('rpmCurrent', rpmCurrent)
          ..add('rpmLimit', rpmLimit)
          ..add('dailyCostUsed', dailyCostUsed)
          ..add('dailyCostLimit', dailyCostLimit)
          ..add('hourlyCostUsed', hourlyCostUsed)
          ..add('hourlyCostLimit', hourlyCostLimit)
          ..add('perTaskCost', perTaskCost)
          ..add('perTaskBudget', perTaskBudget)
          ..add('perSessionCost', perSessionCost)
          ..add('perSessionBudget', perSessionBudget)
          ..add('registeredMethods', registeredMethods)
          ..add('busSubscribers', busSubscribers))
        .toString();
  }
}

class DaemonStatusBuilder
    implements Builder<DaemonStatus, DaemonStatusBuilder> {
  _$DaemonStatus? _$v;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  int? _pidCommaOmitempty;
  int? get pidCommaOmitempty => _$this._pidCommaOmitempty;
  set pidCommaOmitempty(int? pidCommaOmitempty) =>
      _$this._pidCommaOmitempty = pidCommaOmitempty;

  num? _uptimeSecondsCommaOmitempty;
  num? get uptimeSecondsCommaOmitempty => _$this._uptimeSecondsCommaOmitempty;
  set uptimeSecondsCommaOmitempty(num? uptimeSecondsCommaOmitempty) =>
      _$this._uptimeSecondsCommaOmitempty = uptimeSecondsCommaOmitempty;

  String? _modelCommaOmitempty;
  String? get modelCommaOmitempty => _$this._modelCommaOmitempty;
  set modelCommaOmitempty(String? modelCommaOmitempty) =>
      _$this._modelCommaOmitempty = modelCommaOmitempty;

  int? _tokensUsed;
  int? get tokensUsed => _$this._tokensUsed;
  set tokensUsed(int? tokensUsed) => _$this._tokensUsed = tokensUsed;

  int? _tokensRemaining;
  int? get tokensRemaining => _$this._tokensRemaining;
  set tokensRemaining(int? tokensRemaining) =>
      _$this._tokensRemaining = tokensRemaining;

  num? _budgetUsed;
  num? get budgetUsed => _$this._budgetUsed;
  set budgetUsed(num? budgetUsed) => _$this._budgetUsed = budgetUsed;

  num? _budgetRemaining;
  num? get budgetRemaining => _$this._budgetRemaining;
  set budgetRemaining(num? budgetRemaining) =>
      _$this._budgetRemaining = budgetRemaining;

  int? _hourlyUsed;
  int? get hourlyUsed => _$this._hourlyUsed;
  set hourlyUsed(int? hourlyUsed) => _$this._hourlyUsed = hourlyUsed;

  int? _hourlyRemaining;
  int? get hourlyRemaining => _$this._hourlyRemaining;
  set hourlyRemaining(int? hourlyRemaining) =>
      _$this._hourlyRemaining = hourlyRemaining;

  int? _dailyUsed;
  int? get dailyUsed => _$this._dailyUsed;
  set dailyUsed(int? dailyUsed) => _$this._dailyUsed = dailyUsed;

  int? _dailyRemaining;
  int? get dailyRemaining => _$this._dailyRemaining;
  set dailyRemaining(int? dailyRemaining) =>
      _$this._dailyRemaining = dailyRemaining;

  int? _rpmCurrent;
  int? get rpmCurrent => _$this._rpmCurrent;
  set rpmCurrent(int? rpmCurrent) => _$this._rpmCurrent = rpmCurrent;

  int? _rpmLimit;
  int? get rpmLimit => _$this._rpmLimit;
  set rpmLimit(int? rpmLimit) => _$this._rpmLimit = rpmLimit;

  num? _dailyCostUsed;
  num? get dailyCostUsed => _$this._dailyCostUsed;
  set dailyCostUsed(num? dailyCostUsed) =>
      _$this._dailyCostUsed = dailyCostUsed;

  num? _dailyCostLimit;
  num? get dailyCostLimit => _$this._dailyCostLimit;
  set dailyCostLimit(num? dailyCostLimit) =>
      _$this._dailyCostLimit = dailyCostLimit;

  num? _hourlyCostUsed;
  num? get hourlyCostUsed => _$this._hourlyCostUsed;
  set hourlyCostUsed(num? hourlyCostUsed) =>
      _$this._hourlyCostUsed = hourlyCostUsed;

  num? _hourlyCostLimit;
  num? get hourlyCostLimit => _$this._hourlyCostLimit;
  set hourlyCostLimit(num? hourlyCostLimit) =>
      _$this._hourlyCostLimit = hourlyCostLimit;

  num? _perTaskCost;
  num? get perTaskCost => _$this._perTaskCost;
  set perTaskCost(num? perTaskCost) => _$this._perTaskCost = perTaskCost;

  int? _perTaskBudget;
  int? get perTaskBudget => _$this._perTaskBudget;
  set perTaskBudget(int? perTaskBudget) =>
      _$this._perTaskBudget = perTaskBudget;

  num? _perSessionCost;
  num? get perSessionCost => _$this._perSessionCost;
  set perSessionCost(num? perSessionCost) =>
      _$this._perSessionCost = perSessionCost;

  int? _perSessionBudget;
  int? get perSessionBudget => _$this._perSessionBudget;
  set perSessionBudget(int? perSessionBudget) =>
      _$this._perSessionBudget = perSessionBudget;

  int? _registeredMethods;
  int? get registeredMethods => _$this._registeredMethods;
  set registeredMethods(int? registeredMethods) =>
      _$this._registeredMethods = registeredMethods;

  int? _busSubscribers;
  int? get busSubscribers => _$this._busSubscribers;
  set busSubscribers(int? busSubscribers) =>
      _$this._busSubscribers = busSubscribers;

  DaemonStatusBuilder() {
    DaemonStatus._defaults(this);
  }

  DaemonStatusBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _status = $v.status;
      _pidCommaOmitempty = $v.pidCommaOmitempty;
      _uptimeSecondsCommaOmitempty = $v.uptimeSecondsCommaOmitempty;
      _modelCommaOmitempty = $v.modelCommaOmitempty;
      _tokensUsed = $v.tokensUsed;
      _tokensRemaining = $v.tokensRemaining;
      _budgetUsed = $v.budgetUsed;
      _budgetRemaining = $v.budgetRemaining;
      _hourlyUsed = $v.hourlyUsed;
      _hourlyRemaining = $v.hourlyRemaining;
      _dailyUsed = $v.dailyUsed;
      _dailyRemaining = $v.dailyRemaining;
      _rpmCurrent = $v.rpmCurrent;
      _rpmLimit = $v.rpmLimit;
      _dailyCostUsed = $v.dailyCostUsed;
      _dailyCostLimit = $v.dailyCostLimit;
      _hourlyCostUsed = $v.hourlyCostUsed;
      _hourlyCostLimit = $v.hourlyCostLimit;
      _perTaskCost = $v.perTaskCost;
      _perTaskBudget = $v.perTaskBudget;
      _perSessionCost = $v.perSessionCost;
      _perSessionBudget = $v.perSessionBudget;
      _registeredMethods = $v.registeredMethods;
      _busSubscribers = $v.busSubscribers;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(DaemonStatus other) {
    _$v = other as _$DaemonStatus;
  }

  @override
  void update(void Function(DaemonStatusBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  DaemonStatus build() => _build();

  _$DaemonStatus _build() {
    final _$result = _$v ??
        _$DaemonStatus._(
          status: BuiltValueNullFieldError.checkNotNull(
              status, r'DaemonStatus', 'status'),
          pidCommaOmitempty: pidCommaOmitempty,
          uptimeSecondsCommaOmitempty: uptimeSecondsCommaOmitempty,
          modelCommaOmitempty: modelCommaOmitempty,
          tokensUsed: BuiltValueNullFieldError.checkNotNull(
              tokensUsed, r'DaemonStatus', 'tokensUsed'),
          tokensRemaining: BuiltValueNullFieldError.checkNotNull(
              tokensRemaining, r'DaemonStatus', 'tokensRemaining'),
          budgetUsed: BuiltValueNullFieldError.checkNotNull(
              budgetUsed, r'DaemonStatus', 'budgetUsed'),
          budgetRemaining: BuiltValueNullFieldError.checkNotNull(
              budgetRemaining, r'DaemonStatus', 'budgetRemaining'),
          hourlyUsed: hourlyUsed,
          hourlyRemaining: hourlyRemaining,
          dailyUsed: dailyUsed,
          dailyRemaining: dailyRemaining,
          rpmCurrent: rpmCurrent,
          rpmLimit: rpmLimit,
          dailyCostUsed: dailyCostUsed,
          dailyCostLimit: dailyCostLimit,
          hourlyCostUsed: hourlyCostUsed,
          hourlyCostLimit: hourlyCostLimit,
          perTaskCost: perTaskCost,
          perTaskBudget: perTaskBudget,
          perSessionCost: perSessionCost,
          perSessionBudget: perSessionBudget,
          registeredMethods: BuiltValueNullFieldError.checkNotNull(
              registeredMethods, r'DaemonStatus', 'registeredMethods'),
          busSubscribers: BuiltValueNullFieldError.checkNotNull(
              busSubscribers, r'DaemonStatus', 'busSubscribers'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
