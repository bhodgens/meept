//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'publish_request.g.dart';

/// PublishRequest
///
/// Properties:
/// * [topic] 
/// * [type] 
/// * [sourceCommaOmitempty] 
/// * [payloadCommaOmitempty] 
@BuiltValue()
abstract class PublishRequest implements Built<PublishRequest, PublishRequestBuilder> {
  @BuiltValueField(wireName: r'topic')
  String get topic;

  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'source,omitempty')
  String? get sourceCommaOmitempty;

  @BuiltValueField(wireName: r'payload,omitempty')
  String? get payloadCommaOmitempty;

  PublishRequest._();

  factory PublishRequest([void updates(PublishRequestBuilder b)]) = _$PublishRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PublishRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PublishRequest> get serializer => _$PublishRequestSerializer();
}

class _$PublishRequestSerializer implements PrimitiveSerializer<PublishRequest> {
  @override
  final Iterable<Type> types = const [PublishRequest, _$PublishRequest];

  @override
  final String wireName = r'PublishRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PublishRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'topic';
    yield serializers.serialize(
      object.topic,
      specifiedType: const FullType(String),
    );
    yield r'type';
    yield serializers.serialize(
      object.type,
      specifiedType: const FullType(String),
    );
    if (object.sourceCommaOmitempty != null) {
      yield r'source,omitempty';
      yield serializers.serialize(
        object.sourceCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.payloadCommaOmitempty != null) {
      yield r'payload,omitempty';
      yield serializers.serialize(
        object.payloadCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PublishRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PublishRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'topic':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.topic = valueDes;
          break;
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.type = valueDes;
          break;
        case r'source,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sourceCommaOmitempty = valueDes;
          break;
        case r'payload,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.payloadCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PublishRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PublishRequestBuilder();
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

