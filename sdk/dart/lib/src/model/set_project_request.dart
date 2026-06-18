//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'set_project_request.g.dart';

/// SetProjectRequest
///
/// Properties:
/// * [sessionId] 
/// * [projectId] 
@BuiltValue()
abstract class SetProjectRequest implements Built<SetProjectRequest, SetProjectRequestBuilder> {
  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  @BuiltValueField(wireName: r'project_id')
  String get projectId;

  SetProjectRequest._();

  factory SetProjectRequest([void updates(SetProjectRequestBuilder b)]) = _$SetProjectRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SetProjectRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SetProjectRequest> get serializer => _$SetProjectRequestSerializer();
}

class _$SetProjectRequestSerializer implements PrimitiveSerializer<SetProjectRequest> {
  @override
  final Iterable<Type> types = const [SetProjectRequest, _$SetProjectRequest];

  @override
  final String wireName = r'SetProjectRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SetProjectRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'session_id';
    yield serializers.serialize(
      object.sessionId,
      specifiedType: const FullType(String),
    );
    yield r'project_id';
    yield serializers.serialize(
      object.projectId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    SetProjectRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SetProjectRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'session_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionId = valueDes;
          break;
        case r'project_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.projectId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SetProjectRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SetProjectRequestBuilder();
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

