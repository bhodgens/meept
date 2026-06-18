//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_clear_request.g.dart';

/// TemplatesClearRequest
///
/// Properties:
/// * [conversationId] 
/// * [nameCommaOmitempty] 
@BuiltValue()
abstract class TemplatesClearRequest implements Built<TemplatesClearRequest, TemplatesClearRequestBuilder> {
  @BuiltValueField(wireName: r'conversation_id')
  String get conversationId;

  @BuiltValueField(wireName: r'name,omitempty')
  String? get nameCommaOmitempty;

  TemplatesClearRequest._();

  factory TemplatesClearRequest([void updates(TemplatesClearRequestBuilder b)]) = _$TemplatesClearRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesClearRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesClearRequest> get serializer => _$TemplatesClearRequestSerializer();
}

class _$TemplatesClearRequestSerializer implements PrimitiveSerializer<TemplatesClearRequest> {
  @override
  final Iterable<Type> types = const [TemplatesClearRequest, _$TemplatesClearRequest];

  @override
  final String wireName = r'TemplatesClearRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesClearRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'conversation_id';
    yield serializers.serialize(
      object.conversationId,
      specifiedType: const FullType(String),
    );
    if (object.nameCommaOmitempty != null) {
      yield r'name,omitempty';
      yield serializers.serialize(
        object.nameCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplatesClearRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesClearRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'conversation_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.conversationId = valueDes;
          break;
        case r'name,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.nameCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TemplatesClearRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesClearRequestBuilder();
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

