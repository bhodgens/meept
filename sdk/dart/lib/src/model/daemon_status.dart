//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'daemon_status.g.dart';

/// DaemonStatus
///
/// Properties:
/// * [status] 
/// * [pidCommaOmitempty] 
/// * [uptimeSecondsCommaOmitempty] 
/// * [modelCommaOmitempty] 
/// * [tokensUsed] 
/// * [tokensRemaining] 
/// * [budgetUsed] 
/// * [budgetRemaining] 
/// * [hourlyUsed] 
/// * [hourlyRemaining] 
/// * [dailyUsed] 
/// * [dailyRemaining] 
/// * [rpmCurrent] 
/// * [rpmLimit] 
/// * [dailyCostUsed] 
/// * [dailyCostLimit] 
/// * [hourlyCostUsed] 
/// * [hourlyCostLimit] 
/// * [perTaskCost] 
/// * [perTaskBudget] 
/// * [perSessionCost] 
/// * [perSessionBudget] 
/// * [registeredMethods] 
/// * [busSubscribers] 
@BuiltValue()
abstract class DaemonStatus implements Built<DaemonStatus, DaemonStatusBuilder> {
  @BuiltValueField(wireName: r'status')
  String get status;

  @BuiltValueField(wireName: r'pid,omitempty')
  int? get pidCommaOmitempty;

  @BuiltValueField(wireName: r'uptime_seconds,omitempty')
  num? get uptimeSecondsCommaOmitempty;

  @BuiltValueField(wireName: r'model,omitempty')
  String? get modelCommaOmitempty;

  @BuiltValueField(wireName: r'tokens_used')
  int get tokensUsed;

  @BuiltValueField(wireName: r'tokens_remaining')
  int get tokensRemaining;

  @BuiltValueField(wireName: r'budget_used')
  num get budgetUsed;

  @BuiltValueField(wireName: r'budget_remaining')
  num get budgetRemaining;

  @BuiltValueField(wireName: r'hourly_used')
  int? get hourlyUsed;

  @BuiltValueField(wireName: r'hourly_remaining')
  int? get hourlyRemaining;

  @BuiltValueField(wireName: r'daily_used')
  int? get dailyUsed;

  @BuiltValueField(wireName: r'daily_remaining')
  int? get dailyRemaining;

  @BuiltValueField(wireName: r'rpm_current')
  int? get rpmCurrent;

  @BuiltValueField(wireName: r'rpm_limit')
  int? get rpmLimit;

  @BuiltValueField(wireName: r'daily_cost_used')
  num? get dailyCostUsed;

  @BuiltValueField(wireName: r'daily_cost_limit')
  num? get dailyCostLimit;

  @BuiltValueField(wireName: r'hourly_cost_used')
  num? get hourlyCostUsed;

  @BuiltValueField(wireName: r'hourly_cost_limit')
  num? get hourlyCostLimit;

  @BuiltValueField(wireName: r'per_task_cost')
  num? get perTaskCost;

  @BuiltValueField(wireName: r'per_task_budget')
  int? get perTaskBudget;

  @BuiltValueField(wireName: r'per_session_cost')
  num? get perSessionCost;

  @BuiltValueField(wireName: r'per_session_budget')
  int? get perSessionBudget;

  @BuiltValueField(wireName: r'registered_methods')
  int get registeredMethods;

  @BuiltValueField(wireName: r'bus_subscribers')
  int get busSubscribers;

  DaemonStatus._();

  factory DaemonStatus([void updates(DaemonStatusBuilder b)]) = _$DaemonStatus;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(DaemonStatusBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<DaemonStatus> get serializer => _$DaemonStatusSerializer();
}

class _$DaemonStatusSerializer implements PrimitiveSerializer<DaemonStatus> {
  @override
  final Iterable<Type> types = const [DaemonStatus, _$DaemonStatus];

  @override
  final String wireName = r'DaemonStatus';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    DaemonStatus object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'status';
    yield serializers.serialize(
      object.status,
      specifiedType: const FullType(String),
    );
    if (object.pidCommaOmitempty != null) {
      yield r'pid,omitempty';
      yield serializers.serialize(
        object.pidCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
    if (object.uptimeSecondsCommaOmitempty != null) {
      yield r'uptime_seconds,omitempty';
      yield serializers.serialize(
        object.uptimeSecondsCommaOmitempty,
        specifiedType: const FullType(num),
      );
    }
    if (object.modelCommaOmitempty != null) {
      yield r'model,omitempty';
      yield serializers.serialize(
        object.modelCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'tokens_used';
    yield serializers.serialize(
      object.tokensUsed,
      specifiedType: const FullType(int),
    );
    yield r'tokens_remaining';
    yield serializers.serialize(
      object.tokensRemaining,
      specifiedType: const FullType(int),
    );
    yield r'budget_used';
    yield serializers.serialize(
      object.budgetUsed,
      specifiedType: const FullType(num),
    );
    yield r'budget_remaining';
    yield serializers.serialize(
      object.budgetRemaining,
      specifiedType: const FullType(num),
    );
    if (object.hourlyUsed != null) {
      yield r'hourly_used';
      yield serializers.serialize(
        object.hourlyUsed,
        specifiedType: const FullType(int),
      );
    }
    if (object.hourlyRemaining != null) {
      yield r'hourly_remaining';
      yield serializers.serialize(
        object.hourlyRemaining,
        specifiedType: const FullType(int),
      );
    }
    if (object.dailyUsed != null) {
      yield r'daily_used';
      yield serializers.serialize(
        object.dailyUsed,
        specifiedType: const FullType(int),
      );
    }
    if (object.dailyRemaining != null) {
      yield r'daily_remaining';
      yield serializers.serialize(
        object.dailyRemaining,
        specifiedType: const FullType(int),
      );
    }
    if (object.rpmCurrent != null) {
      yield r'rpm_current';
      yield serializers.serialize(
        object.rpmCurrent,
        specifiedType: const FullType(int),
      );
    }
    if (object.rpmLimit != null) {
      yield r'rpm_limit';
      yield serializers.serialize(
        object.rpmLimit,
        specifiedType: const FullType(int),
      );
    }
    if (object.dailyCostUsed != null) {
      yield r'daily_cost_used';
      yield serializers.serialize(
        object.dailyCostUsed,
        specifiedType: const FullType(num),
      );
    }
    if (object.dailyCostLimit != null) {
      yield r'daily_cost_limit';
      yield serializers.serialize(
        object.dailyCostLimit,
        specifiedType: const FullType(num),
      );
    }
    if (object.hourlyCostUsed != null) {
      yield r'hourly_cost_used';
      yield serializers.serialize(
        object.hourlyCostUsed,
        specifiedType: const FullType(num),
      );
    }
    if (object.hourlyCostLimit != null) {
      yield r'hourly_cost_limit';
      yield serializers.serialize(
        object.hourlyCostLimit,
        specifiedType: const FullType(num),
      );
    }
    if (object.perTaskCost != null) {
      yield r'per_task_cost';
      yield serializers.serialize(
        object.perTaskCost,
        specifiedType: const FullType(num),
      );
    }
    if (object.perTaskBudget != null) {
      yield r'per_task_budget';
      yield serializers.serialize(
        object.perTaskBudget,
        specifiedType: const FullType(int),
      );
    }
    if (object.perSessionCost != null) {
      yield r'per_session_cost';
      yield serializers.serialize(
        object.perSessionCost,
        specifiedType: const FullType(num),
      );
    }
    if (object.perSessionBudget != null) {
      yield r'per_session_budget';
      yield serializers.serialize(
        object.perSessionBudget,
        specifiedType: const FullType(int),
      );
    }
    yield r'registered_methods';
    yield serializers.serialize(
      object.registeredMethods,
      specifiedType: const FullType(int),
    );
    yield r'bus_subscribers';
    yield serializers.serialize(
      object.busSubscribers,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    DaemonStatus object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required DaemonStatusBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'status':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.status = valueDes;
          break;
        case r'pid,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.pidCommaOmitempty = valueDes;
          break;
        case r'uptime_seconds,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.uptimeSecondsCommaOmitempty = valueDes;
          break;
        case r'model,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.modelCommaOmitempty = valueDes;
          break;
        case r'tokens_used':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.tokensUsed = valueDes;
          break;
        case r'tokens_remaining':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.tokensRemaining = valueDes;
          break;
        case r'budget_used':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.budgetUsed = valueDes;
          break;
        case r'budget_remaining':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.budgetRemaining = valueDes;
          break;
        case r'hourly_used':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.hourlyUsed = valueDes;
          break;
        case r'hourly_remaining':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.hourlyRemaining = valueDes;
          break;
        case r'daily_used':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.dailyUsed = valueDes;
          break;
        case r'daily_remaining':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.dailyRemaining = valueDes;
          break;
        case r'rpm_current':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.rpmCurrent = valueDes;
          break;
        case r'rpm_limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.rpmLimit = valueDes;
          break;
        case r'daily_cost_used':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.dailyCostUsed = valueDes;
          break;
        case r'daily_cost_limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.dailyCostLimit = valueDes;
          break;
        case r'hourly_cost_used':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.hourlyCostUsed = valueDes;
          break;
        case r'hourly_cost_limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.hourlyCostLimit = valueDes;
          break;
        case r'per_task_cost':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.perTaskCost = valueDes;
          break;
        case r'per_task_budget':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.perTaskBudget = valueDes;
          break;
        case r'per_session_cost':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.perSessionCost = valueDes;
          break;
        case r'per_session_budget':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.perSessionBudget = valueDes;
          break;
        case r'registered_methods':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.registeredMethods = valueDes;
          break;
        case r'bus_subscribers':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.busSubscribers = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  DaemonStatus deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = DaemonStatusBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

