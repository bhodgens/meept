//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'follow_up_request.g.dart';

/// FollowUpRequest
///
/// Properties:
/// * [message] 
/// * [conversationId] 
/// * [sourceCommaOmitempty] 
@BuiltValue()
abstract class FollowUpRequest implements Built<FollowUpRequest, FollowUpRequestBuilder> {
  @BuiltValueField(wireName: r'message')
  String get message;

  @BuiltValueField(wireName: r'conversation_id')
  String get conversationId;

  @BuiltValueField(wireName: r'source,omitempty')
  String? get sourceCommaOmitempty;

  FollowUpRequest._();

  factory FollowUpRequest([void updates(FollowUpRequestBuilder b)]) = _$FollowUpRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(FollowUpRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<FollowUpRequest> get serializer => _$FollowUpRequestSerializer();
}

class _$FollowUpRequestSerializer implements PrimitiveSerializer<FollowUpRequest> {
  @override
  final Iterable<Type> types = const [FollowUpRequest, _$FollowUpRequest];

  @override
  final String wireName = r'FollowUpRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    FollowUpRequest object, {
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
    FollowUpRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required FollowUpRequestBuilder result,
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
  FollowUpRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = FollowUpRequestBuilder();
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

