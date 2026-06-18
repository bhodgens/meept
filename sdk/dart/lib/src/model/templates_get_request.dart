//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_get_request.g.dart';

/// TemplatesGetRequest
///
/// Properties:
/// * [name] 
@BuiltValue()
abstract class TemplatesGetRequest implements Built<TemplatesGetRequest, TemplatesGetRequestBuilder> {
  @BuiltValueField(wireName: r'name')
  String get name;

  TemplatesGetRequest._();

  factory TemplatesGetRequest([void updates(TemplatesGetRequestBuilder b)]) = _$TemplatesGetRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesGetRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesGetRequest> get serializer => _$TemplatesGetRequestSerializer();
}

class _$TemplatesGetRequestSerializer implements PrimitiveSerializer<TemplatesGetRequest> {
  @override
  final Iterable<Type> types = const [TemplatesGetRequest, _$TemplatesGetRequest];

  @override
  final String wireName = r'TemplatesGetRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesGetRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplatesGetRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesGetRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
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
  TemplatesGetRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesGetRequestBuilder();
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

