//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'skills_get_request.g.dart';

/// SkillsGetRequest
///
/// Properties:
/// * [slug] 
@BuiltValue()
abstract class SkillsGetRequest implements Built<SkillsGetRequest, SkillsGetRequestBuilder> {
  @BuiltValueField(wireName: r'slug')
  String get slug;

  SkillsGetRequest._();

  factory SkillsGetRequest([void updates(SkillsGetRequestBuilder b)]) = _$SkillsGetRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SkillsGetRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SkillsGetRequest> get serializer => _$SkillsGetRequestSerializer();
}

class _$SkillsGetRequestSerializer implements PrimitiveSerializer<SkillsGetRequest> {
  @override
  final Iterable<Type> types = const [SkillsGetRequest, _$SkillsGetRequest];

  @override
  final String wireName = r'SkillsGetRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SkillsGetRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'slug';
    yield serializers.serialize(
      object.slug,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    SkillsGetRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SkillsGetRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'slug':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.slug = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SkillsGetRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SkillsGetRequestBuilder();
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

