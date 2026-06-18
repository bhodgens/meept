//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'project_service.g.dart';

/// ProjectService
///
/// Properties:
/// * [pm] 
/// * [store] 
@BuiltValue()
abstract class ProjectService implements Built<ProjectService, ProjectServiceBuilder> {
  @BuiltValueField(wireName: r'pm')
  JsonObject? get pm;

  @BuiltValueField(wireName: r'store')
  JsonObject? get store;

  ProjectService._();

  factory ProjectService([void updates(ProjectServiceBuilder b)]) = _$ProjectService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ProjectServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ProjectService> get serializer => _$ProjectServiceSerializer();
}

class _$ProjectServiceSerializer implements PrimitiveSerializer<ProjectService> {
  @override
  final Iterable<Type> types = const [ProjectService, _$ProjectService];

  @override
  final String wireName = r'ProjectService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ProjectService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.pm != null) {
      yield r'pm';
      yield serializers.serialize(
        object.pm,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.store != null) {
      yield r'store';
      yield serializers.serialize(
        object.store,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ProjectService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ProjectServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'pm':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.pm = valueDes;
          break;
        case r'store':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.store = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ProjectService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ProjectServiceBuilder();
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

