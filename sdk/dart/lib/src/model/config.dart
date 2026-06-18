//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'config.g.dart';

/// Config
///
/// Properties:
/// * [bus] 
/// * [agentRegistry] 
/// * [queue] 
/// * [memoryManager] 
/// * [taskRegistry] 
/// * [sessionStore] 
/// * [workerPool] 
/// * [skillRegistry] 
/// * [skillExecutor] 
/// * [templateRegistry] 
/// * [selfImprove] 
/// * [tokenCache] 
/// * [securityChecker] 
/// * [scheduler] 
/// * [calendarClient] 
/// * [daemonController] 
/// * [runtimeManager] 
/// * [workingDir] 
/// * [pidFile] 
/// * [stateDir] 
/// * [binPath] 
/// * [projectManager] 
/// * [planManager] 
/// * [planStore] 
@BuiltValue()
abstract class Config implements Built<Config, ConfigBuilder> {
  @BuiltValueField(wireName: r'Bus')
  JsonObject? get bus;

  @BuiltValueField(wireName: r'AgentRegistry')
  JsonObject? get agentRegistry;

  @BuiltValueField(wireName: r'Queue')
  JsonObject? get queue;

  @BuiltValueField(wireName: r'MemoryManager')
  JsonObject? get memoryManager;

  @BuiltValueField(wireName: r'TaskRegistry')
  JsonObject? get taskRegistry;

  @BuiltValueField(wireName: r'SessionStore')
  JsonObject? get sessionStore;

  @BuiltValueField(wireName: r'WorkerPool')
  JsonObject? get workerPool;

  @BuiltValueField(wireName: r'SkillRegistry')
  JsonObject? get skillRegistry;

  @BuiltValueField(wireName: r'SkillExecutor')
  JsonObject? get skillExecutor;

  @BuiltValueField(wireName: r'TemplateRegistry')
  JsonObject? get templateRegistry;

  @BuiltValueField(wireName: r'SelfImprove')
  JsonObject? get selfImprove;

  @BuiltValueField(wireName: r'TokenCache')
  JsonObject? get tokenCache;

  @BuiltValueField(wireName: r'SecurityChecker')
  JsonObject? get securityChecker;

  @BuiltValueField(wireName: r'Scheduler')
  JsonObject? get scheduler;

  @BuiltValueField(wireName: r'CalendarClient')
  JsonObject? get calendarClient;

  @BuiltValueField(wireName: r'DaemonController')
  JsonObject? get daemonController;

  @BuiltValueField(wireName: r'RuntimeManager')
  JsonObject? get runtimeManager;

  @BuiltValueField(wireName: r'WorkingDir')
  String? get workingDir;

  @BuiltValueField(wireName: r'PidFile')
  String? get pidFile;

  @BuiltValueField(wireName: r'StateDir')
  String? get stateDir;

  @BuiltValueField(wireName: r'BinPath')
  String? get binPath;

  @BuiltValueField(wireName: r'ProjectManager')
  JsonObject? get projectManager;

  @BuiltValueField(wireName: r'PlanManager')
  JsonObject? get planManager;

  @BuiltValueField(wireName: r'PlanStore')
  JsonObject? get planStore;

  Config._();

  factory Config([void updates(ConfigBuilder b)]) = _$Config;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ConfigBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<Config> get serializer => _$ConfigSerializer();
}

class _$ConfigSerializer implements PrimitiveSerializer<Config> {
  @override
  final Iterable<Type> types = const [Config, _$Config];

  @override
  final String wireName = r'Config';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    Config object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.bus != null) {
      yield r'Bus';
      yield serializers.serialize(
        object.bus,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.agentRegistry != null) {
      yield r'AgentRegistry';
      yield serializers.serialize(
        object.agentRegistry,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.queue != null) {
      yield r'Queue';
      yield serializers.serialize(
        object.queue,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.memoryManager != null) {
      yield r'MemoryManager';
      yield serializers.serialize(
        object.memoryManager,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.taskRegistry != null) {
      yield r'TaskRegistry';
      yield serializers.serialize(
        object.taskRegistry,
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
    if (object.workerPool != null) {
      yield r'WorkerPool';
      yield serializers.serialize(
        object.workerPool,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.skillRegistry != null) {
      yield r'SkillRegistry';
      yield serializers.serialize(
        object.skillRegistry,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.skillExecutor != null) {
      yield r'SkillExecutor';
      yield serializers.serialize(
        object.skillExecutor,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.templateRegistry != null) {
      yield r'TemplateRegistry';
      yield serializers.serialize(
        object.templateRegistry,
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
    if (object.tokenCache != null) {
      yield r'TokenCache';
      yield serializers.serialize(
        object.tokenCache,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.securityChecker != null) {
      yield r'SecurityChecker';
      yield serializers.serialize(
        object.securityChecker,
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
    if (object.calendarClient != null) {
      yield r'CalendarClient';
      yield serializers.serialize(
        object.calendarClient,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.daemonController != null) {
      yield r'DaemonController';
      yield serializers.serialize(
        object.daemonController,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.runtimeManager != null) {
      yield r'RuntimeManager';
      yield serializers.serialize(
        object.runtimeManager,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.workingDir != null) {
      yield r'WorkingDir';
      yield serializers.serialize(
        object.workingDir,
        specifiedType: const FullType(String),
      );
    }
    if (object.pidFile != null) {
      yield r'PidFile';
      yield serializers.serialize(
        object.pidFile,
        specifiedType: const FullType(String),
      );
    }
    if (object.stateDir != null) {
      yield r'StateDir';
      yield serializers.serialize(
        object.stateDir,
        specifiedType: const FullType(String),
      );
    }
    if (object.binPath != null) {
      yield r'BinPath';
      yield serializers.serialize(
        object.binPath,
        specifiedType: const FullType(String),
      );
    }
    if (object.projectManager != null) {
      yield r'ProjectManager';
      yield serializers.serialize(
        object.projectManager,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.planManager != null) {
      yield r'PlanManager';
      yield serializers.serialize(
        object.planManager,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.planStore != null) {
      yield r'PlanStore';
      yield serializers.serialize(
        object.planStore,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    Config object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ConfigBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'Bus':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.bus = valueDes;
          break;
        case r'AgentRegistry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.agentRegistry = valueDes;
          break;
        case r'Queue':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.queue = valueDes;
          break;
        case r'MemoryManager':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.memoryManager = valueDes;
          break;
        case r'TaskRegistry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.taskRegistry = valueDes;
          break;
        case r'SessionStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.sessionStore = valueDes;
          break;
        case r'WorkerPool':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.workerPool = valueDes;
          break;
        case r'SkillRegistry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.skillRegistry = valueDes;
          break;
        case r'SkillExecutor':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.skillExecutor = valueDes;
          break;
        case r'TemplateRegistry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.templateRegistry = valueDes;
          break;
        case r'SelfImprove':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.selfImprove = valueDes;
          break;
        case r'TokenCache':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.tokenCache = valueDes;
          break;
        case r'SecurityChecker':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.securityChecker = valueDes;
          break;
        case r'Scheduler':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.scheduler = valueDes;
          break;
        case r'CalendarClient':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.calendarClient = valueDes;
          break;
        case r'DaemonController':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.daemonController = valueDes;
          break;
        case r'RuntimeManager':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.runtimeManager = valueDes;
          break;
        case r'WorkingDir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workingDir = valueDes;
          break;
        case r'PidFile':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.pidFile = valueDes;
          break;
        case r'StateDir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.stateDir = valueDes;
          break;
        case r'BinPath':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.binPath = valueDes;
          break;
        case r'ProjectManager':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.projectManager = valueDes;
          break;
        case r'PlanManager':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.planManager = valueDes;
          break;
        case r'PlanStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.planStore = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  Config deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ConfigBuilder();
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

