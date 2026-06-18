//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'attach_session_request.g.dart';

/// AttachSessionRequest
///
/// Properties:
/// * [id] 
/// * [agentId] 
@BuiltValue()
abstract class AttachSessionRequest implements Built<AttachSessionRequest, AttachSessionRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'agent_id')
  String get agentId;

  AttachSessionRequest._();

  factory AttachSessionRequest([void updates(AttachSessionRequestBuilder b)]) = _$AttachSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AttachSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AttachSessionRequest> get serializer => _$AttachSessionRequestSerializer();
}

class _$AttachSessionRequestSerializer implements PrimitiveSerializer<AttachSessionRequest> {
  @override
  final Iterable<Type> types = const [AttachSessionRequest, _$AttachSessionRequest];

  @override
  final String wireName = r'AttachSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AttachSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'agent_id';
    yield serializers.serialize(
      object.agentId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    AttachSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AttachSessionRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'agent_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.agentId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AttachSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AttachSessionRequestBuilder();
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

