//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DaemonStatus {
  /// Returns a new [DaemonStatus] instance.
  DaemonStatus({
    required this.status,
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
    required this.busSubscribers,
  });

  String status;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? pidCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? uptimeSecondsCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? modelCommaOmitempty;

  int tokensUsed;

  int tokensRemaining;

  num budgetUsed;

  num budgetRemaining;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? hourlyUsed;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? hourlyRemaining;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? dailyUsed;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? dailyRemaining;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? rpmCurrent;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? rpmLimit;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? dailyCostUsed;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? dailyCostLimit;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? hourlyCostUsed;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? hourlyCostLimit;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? perTaskCost;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? perTaskBudget;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? perSessionCost;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? perSessionBudget;

  int registeredMethods;

  int busSubscribers;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DaemonStatus &&
    other.status == status &&
    other.pidCommaOmitempty == pidCommaOmitempty &&
    other.uptimeSecondsCommaOmitempty == uptimeSecondsCommaOmitempty &&
    other.modelCommaOmitempty == modelCommaOmitempty &&
    other.tokensUsed == tokensUsed &&
    other.tokensRemaining == tokensRemaining &&
    other.budgetUsed == budgetUsed &&
    other.budgetRemaining == budgetRemaining &&
    other.hourlyUsed == hourlyUsed &&
    other.hourlyRemaining == hourlyRemaining &&
    other.dailyUsed == dailyUsed &&
    other.dailyRemaining == dailyRemaining &&
    other.rpmCurrent == rpmCurrent &&
    other.rpmLimit == rpmLimit &&
    other.dailyCostUsed == dailyCostUsed &&
    other.dailyCostLimit == dailyCostLimit &&
    other.hourlyCostUsed == hourlyCostUsed &&
    other.hourlyCostLimit == hourlyCostLimit &&
    other.perTaskCost == perTaskCost &&
    other.perTaskBudget == perTaskBudget &&
    other.perSessionCost == perSessionCost &&
    other.perSessionBudget == perSessionBudget &&
    other.registeredMethods == registeredMethods &&
    other.busSubscribers == busSubscribers;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (status.hashCode) +
    (pidCommaOmitempty == null ? 0 : pidCommaOmitempty!.hashCode) +
    (uptimeSecondsCommaOmitempty == null ? 0 : uptimeSecondsCommaOmitempty!.hashCode) +
    (modelCommaOmitempty == null ? 0 : modelCommaOmitempty!.hashCode) +
    (tokensUsed.hashCode) +
    (tokensRemaining.hashCode) +
    (budgetUsed.hashCode) +
    (budgetRemaining.hashCode) +
    (hourlyUsed == null ? 0 : hourlyUsed!.hashCode) +
    (hourlyRemaining == null ? 0 : hourlyRemaining!.hashCode) +
    (dailyUsed == null ? 0 : dailyUsed!.hashCode) +
    (dailyRemaining == null ? 0 : dailyRemaining!.hashCode) +
    (rpmCurrent == null ? 0 : rpmCurrent!.hashCode) +
    (rpmLimit == null ? 0 : rpmLimit!.hashCode) +
    (dailyCostUsed == null ? 0 : dailyCostUsed!.hashCode) +
    (dailyCostLimit == null ? 0 : dailyCostLimit!.hashCode) +
    (hourlyCostUsed == null ? 0 : hourlyCostUsed!.hashCode) +
    (hourlyCostLimit == null ? 0 : hourlyCostLimit!.hashCode) +
    (perTaskCost == null ? 0 : perTaskCost!.hashCode) +
    (perTaskBudget == null ? 0 : perTaskBudget!.hashCode) +
    (perSessionCost == null ? 0 : perSessionCost!.hashCode) +
    (perSessionBudget == null ? 0 : perSessionBudget!.hashCode) +
    (registeredMethods.hashCode) +
    (busSubscribers.hashCode);

  @override
  String toString() => 'DaemonStatus[status=$status, pidCommaOmitempty=$pidCommaOmitempty, uptimeSecondsCommaOmitempty=$uptimeSecondsCommaOmitempty, modelCommaOmitempty=$modelCommaOmitempty, tokensUsed=$tokensUsed, tokensRemaining=$tokensRemaining, budgetUsed=$budgetUsed, budgetRemaining=$budgetRemaining, hourlyUsed=$hourlyUsed, hourlyRemaining=$hourlyRemaining, dailyUsed=$dailyUsed, dailyRemaining=$dailyRemaining, rpmCurrent=$rpmCurrent, rpmLimit=$rpmLimit, dailyCostUsed=$dailyCostUsed, dailyCostLimit=$dailyCostLimit, hourlyCostUsed=$hourlyCostUsed, hourlyCostLimit=$hourlyCostLimit, perTaskCost=$perTaskCost, perTaskBudget=$perTaskBudget, perSessionCost=$perSessionCost, perSessionBudget=$perSessionBudget, registeredMethods=$registeredMethods, busSubscribers=$busSubscribers]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'status'] = this.status;
    if (this.pidCommaOmitempty != null) {
      json[r'pid,omitempty'] = this.pidCommaOmitempty;
    } else {
      json[r'pid,omitempty'] = null;
    }
    if (this.uptimeSecondsCommaOmitempty != null) {
      json[r'uptime_seconds,omitempty'] = this.uptimeSecondsCommaOmitempty;
    } else {
      json[r'uptime_seconds,omitempty'] = null;
    }
    if (this.modelCommaOmitempty != null) {
      json[r'model,omitempty'] = this.modelCommaOmitempty;
    } else {
      json[r'model,omitempty'] = null;
    }
      json[r'tokens_used'] = this.tokensUsed;
      json[r'tokens_remaining'] = this.tokensRemaining;
      json[r'budget_used'] = this.budgetUsed;
      json[r'budget_remaining'] = this.budgetRemaining;
    if (this.hourlyUsed != null) {
      json[r'hourly_used'] = this.hourlyUsed;
    } else {
      json[r'hourly_used'] = null;
    }
    if (this.hourlyRemaining != null) {
      json[r'hourly_remaining'] = this.hourlyRemaining;
    } else {
      json[r'hourly_remaining'] = null;
    }
    if (this.dailyUsed != null) {
      json[r'daily_used'] = this.dailyUsed;
    } else {
      json[r'daily_used'] = null;
    }
    if (this.dailyRemaining != null) {
      json[r'daily_remaining'] = this.dailyRemaining;
    } else {
      json[r'daily_remaining'] = null;
    }
    if (this.rpmCurrent != null) {
      json[r'rpm_current'] = this.rpmCurrent;
    } else {
      json[r'rpm_current'] = null;
    }
    if (this.rpmLimit != null) {
      json[r'rpm_limit'] = this.rpmLimit;
    } else {
      json[r'rpm_limit'] = null;
    }
    if (this.dailyCostUsed != null) {
      json[r'daily_cost_used'] = this.dailyCostUsed;
    } else {
      json[r'daily_cost_used'] = null;
    }
    if (this.dailyCostLimit != null) {
      json[r'daily_cost_limit'] = this.dailyCostLimit;
    } else {
      json[r'daily_cost_limit'] = null;
    }
    if (this.hourlyCostUsed != null) {
      json[r'hourly_cost_used'] = this.hourlyCostUsed;
    } else {
      json[r'hourly_cost_used'] = null;
    }
    if (this.hourlyCostLimit != null) {
      json[r'hourly_cost_limit'] = this.hourlyCostLimit;
    } else {
      json[r'hourly_cost_limit'] = null;
    }
    if (this.perTaskCost != null) {
      json[r'per_task_cost'] = this.perTaskCost;
    } else {
      json[r'per_task_cost'] = null;
    }
    if (this.perTaskBudget != null) {
      json[r'per_task_budget'] = this.perTaskBudget;
    } else {
      json[r'per_task_budget'] = null;
    }
    if (this.perSessionCost != null) {
      json[r'per_session_cost'] = this.perSessionCost;
    } else {
      json[r'per_session_cost'] = null;
    }
    if (this.perSessionBudget != null) {
      json[r'per_session_budget'] = this.perSessionBudget;
    } else {
      json[r'per_session_budget'] = null;
    }
      json[r'registered_methods'] = this.registeredMethods;
      json[r'bus_subscribers'] = this.busSubscribers;
    return json;
  }

  /// Returns a new [DaemonStatus] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DaemonStatus? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'status'), 'Required key "DaemonStatus[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "DaemonStatus[status]" has a null value in JSON.');
        assert(json.containsKey(r'tokens_used'), 'Required key "DaemonStatus[tokens_used]" is missing from JSON.');
        assert(json[r'tokens_used'] != null, 'Required key "DaemonStatus[tokens_used]" has a null value in JSON.');
        assert(json.containsKey(r'tokens_remaining'), 'Required key "DaemonStatus[tokens_remaining]" is missing from JSON.');
        assert(json[r'tokens_remaining'] != null, 'Required key "DaemonStatus[tokens_remaining]" has a null value in JSON.');
        assert(json.containsKey(r'budget_used'), 'Required key "DaemonStatus[budget_used]" is missing from JSON.');
        assert(json[r'budget_used'] != null, 'Required key "DaemonStatus[budget_used]" has a null value in JSON.');
        assert(json.containsKey(r'budget_remaining'), 'Required key "DaemonStatus[budget_remaining]" is missing from JSON.');
        assert(json[r'budget_remaining'] != null, 'Required key "DaemonStatus[budget_remaining]" has a null value in JSON.');
        assert(json.containsKey(r'registered_methods'), 'Required key "DaemonStatus[registered_methods]" is missing from JSON.');
        assert(json[r'registered_methods'] != null, 'Required key "DaemonStatus[registered_methods]" has a null value in JSON.');
        assert(json.containsKey(r'bus_subscribers'), 'Required key "DaemonStatus[bus_subscribers]" is missing from JSON.');
        assert(json[r'bus_subscribers'] != null, 'Required key "DaemonStatus[bus_subscribers]" has a null value in JSON.');
        return true;
      }());

      return DaemonStatus(
        status: mapValueOfType<String>(json, r'status')!,
        pidCommaOmitempty: mapValueOfType<int>(json, r'pid,omitempty'),
        uptimeSecondsCommaOmitempty: num.parse('${json[r'uptime_seconds,omitempty']}'),
        modelCommaOmitempty: mapValueOfType<String>(json, r'model,omitempty'),
        tokensUsed: mapValueOfType<int>(json, r'tokens_used')!,
        tokensRemaining: mapValueOfType<int>(json, r'tokens_remaining')!,
        budgetUsed: num.parse('${json[r'budget_used']}'),
        budgetRemaining: num.parse('${json[r'budget_remaining']}'),
        hourlyUsed: mapValueOfType<int>(json, r'hourly_used'),
        hourlyRemaining: mapValueOfType<int>(json, r'hourly_remaining'),
        dailyUsed: mapValueOfType<int>(json, r'daily_used'),
        dailyRemaining: mapValueOfType<int>(json, r'daily_remaining'),
        rpmCurrent: mapValueOfType<int>(json, r'rpm_current'),
        rpmLimit: mapValueOfType<int>(json, r'rpm_limit'),
        dailyCostUsed: num.parse('${json[r'daily_cost_used']}'),
        dailyCostLimit: num.parse('${json[r'daily_cost_limit']}'),
        hourlyCostUsed: num.parse('${json[r'hourly_cost_used']}'),
        hourlyCostLimit: num.parse('${json[r'hourly_cost_limit']}'),
        perTaskCost: num.parse('${json[r'per_task_cost']}'),
        perTaskBudget: mapValueOfType<int>(json, r'per_task_budget'),
        perSessionCost: num.parse('${json[r'per_session_cost']}'),
        perSessionBudget: mapValueOfType<int>(json, r'per_session_budget'),
        registeredMethods: mapValueOfType<int>(json, r'registered_methods')!,
        busSubscribers: mapValueOfType<int>(json, r'bus_subscribers')!,
      );
    }
    return null;
  }

  static List<DaemonStatus> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DaemonStatus>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DaemonStatus.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DaemonStatus> mapFromJson(dynamic json) {
    final map = <String, DaemonStatus>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DaemonStatus.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DaemonStatus-objects as value to a dart map
  static Map<String, List<DaemonStatus>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DaemonStatus>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DaemonStatus.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'status',
    'tokens_used',
    'tokens_remaining',
    'budget_used',
    'budget_remaining',
    'registered_methods',
    'bus_subscribers',
  };
}

