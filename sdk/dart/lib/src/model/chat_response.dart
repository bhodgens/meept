//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'chat_response.g.dart';

/// ChatResponse
///
/// Properties:
/// * [reply] 
/// * [modelCommaOmitempty] 
/// * [tokensUsedCommaOmitempty] 
@BuiltValue()
abstract class ChatResponse implements Built<ChatResponse, ChatResponseBuilder> {
  @BuiltValueField(wireName: r'reply')
  String get reply;

  @BuiltValueField(wireName: r'model,omitempty')
  String? get modelCommaOmitempty;

  @BuiltValueField(wireName: r'tokens_used,omitempty')
  int? get tokensUsedCommaOmitempty;

  ChatResponse._();

  factory ChatResponse([void updates(ChatResponseBuilder b)]) = _$ChatResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ChatResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ChatResponse> get serializer => _$ChatResponseSerializer();
}

class _$ChatResponseSerializer implements PrimitiveSerializer<ChatResponse> {
  @override
  final Iterable<Type> types = const [ChatResponse, _$ChatResponse];

  @override
  final String wireName = r'ChatResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ChatResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'reply';
    yield serializers.serialize(
      object.reply,
      specifiedType: const FullType(String),
    );
    if (object.modelCommaOmitempty != null) {
      yield r'model,omitempty';
      yield serializers.serialize(
        object.modelCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.tokensUsedCommaOmitempty != null) {
      yield r'tokens_used,omitempty';
      yield serializers.serialize(
        object.tokensUsedCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ChatResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ChatResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'reply':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.reply = valueDes;
          break;
        case r'model,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.modelCommaOmitempty = valueDes;
          break;
        case r'tokens_used,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.tokensUsedCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ChatResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ChatResponseBuilder();
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

