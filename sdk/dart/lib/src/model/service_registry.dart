//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'service_registry.g.dart';

/// ServiceRegistry
///
/// Properties:
/// * [chat] 
/// * [memory] 
/// * [task] 
/// * [queue] 
/// * [session] 
/// * [sessionStore] 
/// * [worker] 
/// * [pipeline] 
/// * [skills] 
/// * [selfImprove] 
/// * [cache] 
/// * [security] 
/// * [scheduler] 
/// * [bus] 
/// * [templates] 
/// * [daemon] 
/// * [model] 
/// * [calendar] 
/// * [runtime] 
/// * [terminal] 
/// * [project] 
/// * [plan] 
/// * [search] 
@BuiltValue()
abstract class ServiceRegistry implements Built<ServiceRegistry, ServiceRegistryBuilder> {
  @BuiltValueField(wireName: r'Chat')
  JsonObject? get chat;

  @BuiltValueField(wireName: r'Memory')
  JsonObject? get memory;

  @BuiltValueField(wireName: r'Task')
  JsonObject? get task;

  @BuiltValueField(wireName: r'Queue')
  JsonObject? get queue;

  @BuiltValueField(wireName: r'Session')
  JsonObject? get session;

  @BuiltValueField(wireName: r'SessionStore')
  JsonObject? get sessionStore;

  @BuiltValueField(wireName: r'Worker')
  JsonObject? get worker;

  @BuiltValueField(wireName: r'Pipeline')
  JsonObject? get pipeline;

  @BuiltValueField(wireName: r'Skills')
  JsonObject? get skills;

  @BuiltValueField(wireName: r'SelfImprove')
  JsonObject? get selfImprove;

  @BuiltValueField(wireName: r'Cache')
  JsonObject? get cache;

  @BuiltValueField(wireName: r'Security')
  JsonObject? get security;

  @BuiltValueField(wireName: r'Scheduler')
  JsonObject? get scheduler;

  @BuiltValueField(wireName: r'Bus')
  JsonObject? get bus;

  @BuiltValueField(wireName: r'Templates')
  JsonObject? get templates;

  @BuiltValueField(wireName: r'Daemon')
  JsonObject? get daemon;

  @BuiltValueField(wireName: r'Model')
  JsonObject? get model;

  @BuiltValueField(wireName: r'Calendar')
  JsonObject? get calendar;

  @BuiltValueField(wireName: r'Runtime')
  JsonObject? get runtime;

  @BuiltValueField(wireName: r'Terminal')
  JsonObject? get terminal;

  @BuiltValueField(wireName: r'Project')
  JsonObject? get project;

  @BuiltValueField(wireName: r'Plan')
  JsonObject? get plan;

  @BuiltValueField(wireName: r'Search')
  JsonObject? get search;

  ServiceRegistry._();

  factory ServiceRegistry([void updates(ServiceRegistryBuilder b)]) = _$ServiceRegistry;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ServiceRegistryBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ServiceRegistry> get serializer => _$ServiceRegistrySerializer();
}

class _$ServiceRegistrySerializer implements PrimitiveSerializer<ServiceRegistry> {
  @override
  final Iterable<Type> types = const [ServiceRegistry, _$ServiceRegistry];

  @override
  final String wireName = r'ServiceRegistry';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ServiceRegistry object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.chat != null) {
      yield r'Chat';
      yield serializers.serialize(
        object.chat,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.memory != null) {
      yield r'Memory';
      yield serializers.serialize(
        object.memory,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.task != null) {
      yield r'Task';
      yield serializers.serialize(
        object.task,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.queue != null) {
      yield r'Queue';
      yield serializers.serialize(
        object.queue,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.session != null) {
      yield r'Session';
      yield serializers.serialize(
        object.session,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.sessionStore != null) {
      yield r'SessionStore';
      yield serializers.serialize(
        object.sessionStore,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.worker != null) {
      yield r'Worker';
      yield serializers.serialize(
        object.worker,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.pipeline != null) {
      yield r'Pipeline';
      yield serializers.serialize(
        object.pipeline,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.skills != null) {
      yield r'Skills';
      yield serializers.serialize(
        object.skills,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.selfImprove != null) {
      yield r'SelfImprove';
      yield serializers.serialize(
        object.selfImprove,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.cache != null) {
      yield r'Cache';
      yield serializers.serialize(
        object.cache,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.security != null) {
      yield r'Security';
      yield serializers.serialize(
        object.security,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.scheduler != null) {
      yield r'Scheduler';
      yield serializers.serialize(
        object.scheduler,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.bus != null) {
      yield r'Bus';
      yield serializers.serialize(
        object.bus,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.templates != null) {
      yield r'Templates';
      yield serializers.serialize(
        object.templates,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.daemon != null) {
      yield r'Daemon';
      yield serializers.serialize(
        object.daemon,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.model != null) {
      yield r'Model';
      yield serializers.serialize(
        object.model,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.calendar != null) {
      yield r'Calendar';
      yield serializers.serialize(
        object.calendar,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.runtime != null) {
      yield r'Runtime';
      yield serializers.serialize(
        object.runtime,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.terminal != null) {
      yield r'Terminal';
      yield serializers.serialize(
        object.terminal,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.project != null) {
      yield r'Project';
      yield serializers.serialize(
        object.project,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.plan != null) {
      yield r'Plan';
      yield serializers.serialize(
        object.plan,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.search != null) {
      yield r'Search';
      yield serializers.serialize(
        object.search,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ServiceRegistry object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ServiceRegistryBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'Chat':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.chat = valueDes;
          break;
        case r'Memory':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.memory = valueDes;
          break;
        case r'Task':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.task = valueDes;
          break;
        case r'Queue':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.queue = valueDes;
          break;
        case r'Session':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.session = valueDes;
          break;
        case r'SessionStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.sessionStore = valueDes;
          break;
        case r'Worker':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.worker = valueDes;
          break;
        case r'Pipeline':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.pipeline = valueDes;
          break;
        case r'Skills':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.skills = valueDes;
          break;
        case r'SelfImprove':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.selfImprove = valueDes;
          break;
        case r'Cache':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.cache = valueDes;
          break;
        case r'Security':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.security = valueDes;
          break;
        case r'Scheduler':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.scheduler = valueDes;
          break;
        case r'Bus':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.bus = valueDes;
          break;
        case r'Templates':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.templates = valueDes;
          break;
        case r'Daemon':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.daemon = valueDes;
          break;
        case r'Model':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.model = valueDes;
          break;
        case r'Calendar':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.calendar = valueDes;
          break;
        case r'Runtime':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.runtime = valueDes;
          break;
        case r'Terminal':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.terminal = valueDes;
          break;
        case r'Project':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.project = valueDes;
          break;
        case r'Plan':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.plan = valueDes;
          break;
        case r'Search':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.search = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ServiceRegistry deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ServiceRegistryBuilder();
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

