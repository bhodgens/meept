//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'steer_request.g.dart';

/// SteerRequest
///
/// Properties:
/// * [message] 
/// * [conversationId] 
/// * [sourceCommaOmitempty] 
@BuiltValue()
abstract class SteerRequest implements Built<SteerRequest, SteerRequestBuilder> {
  @BuiltValueField(wireName: r'message')
  String get message;

  @BuiltValueField(wireName: r'conversation_id')
  String get conversationId;

  @BuiltValueField(wireName: r'source,omitempty')
  String? get sourceCommaOmitempty;

  SteerRequest._();

  factory SteerRequest([void updates(SteerRequestBuilder b)]) = _$SteerRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SteerRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SteerRequest> get serializer => _$SteerRequestSerializer();
}

class _$SteerRequestSerializer implements PrimitiveSerializer<SteerRequest> {
  @override
  final Iterable<Type> types = const [SteerRequest, _$SteerRequest];

  @override
  final String wireName = r'SteerRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SteerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'message';
    yield serializers.serialize(
      object.message,
      specifiedType: const FullType(String),
    );
    yield r'conversation_id';
    yield serializers.serialize(
      object.conversationId,
      specifiedType: const FullType(String),
    );
    if (object.sourceCommaOmitempty != null) {
      yield r'source,omitempty';
      yield serializers.serialize(
        object.sourceCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SteerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SteerRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'message':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.message = valueDes;
          break;
        case r'conversation_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.conversationId = valueDes;
          break;
        case r'source,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sourceCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SteerRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SteerRequestBuilder();
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

