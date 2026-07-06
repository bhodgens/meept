//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'fork_session_request.g.dart';

/// ForkSessionRequest
///
/// Properties:
/// * [sessionId] 
/// * [fromMessageId] 
/// * [name] 
@BuiltValue()
abstract class ForkSessionRequest implements Built<ForkSessionRequest, ForkSessionRequestBuilder> {
  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  @BuiltValueField(wireName: r'from_message_id')
  int get fromMessageId;

  @BuiltValueField(wireName: r'name')
  String? get name;

  ForkSessionRequest._();

  factory ForkSessionRequest([void updates(ForkSessionRequestBuilder b)]) = _$ForkSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ForkSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ForkSessionRequest> get serializer => _$ForkSessionRequestSerializer();
}

class _$ForkSessionRequestSerializer implements PrimitiveSerializer<ForkSessionRequest> {
  @override
  final Iterable<Type> types = const [ForkSessionRequest, _$ForkSessionRequest];

  @override
  final String wireName = r'ForkSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ForkSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'session_id';
    yield serializers.serialize(
      object.sessionId,
      specifiedType: const FullType(String),
    );
    yield r'from_message_id';
    yield serializers.serialize(
      object.fromMessageId,
      specifiedType: const FullType(int),
    );
    if (object.name != null) {
      yield r'name';
      yield serializers.serialize(
        object.name,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ForkSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ForkSessionRequestBuilder result,
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
        case r'from_message_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.fromMessageId = valueDes;
          break;
        case r'name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.name = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ForkSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ForkSessionRequestBuilder();
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

