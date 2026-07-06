//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'skills_list_request.g.dart';

/// SkillsListRequest
///
/// Properties:
/// * [category] 
/// * [limit] 
@BuiltValue()
abstract class SkillsListRequest implements Built<SkillsListRequest, SkillsListRequestBuilder> {
  @BuiltValueField(wireName: r'category')
  String? get category;

  @BuiltValueField(wireName: r'limit')
  int? get limit;

  SkillsListRequest._();

  factory SkillsListRequest([void updates(SkillsListRequestBuilder b)]) = _$SkillsListRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SkillsListRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SkillsListRequest> get serializer => _$SkillsListRequestSerializer();
}

class _$SkillsListRequestSerializer implements PrimitiveSerializer<SkillsListRequest> {
  @override
  final Iterable<Type> types = const [SkillsListRequest, _$SkillsListRequest];

  @override
  final String wireName = r'SkillsListRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SkillsListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.category != null) {
      yield r'category';
      yield serializers.serialize(
        object.category,
        specifiedType: const FullType(String),
      );
    }
    if (object.limit != null) {
      yield r'limit';
      yield serializers.serialize(
        object.limit,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SkillsListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SkillsListRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'category':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.category = valueDes;
          break;
        case r'limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limit = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SkillsListRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SkillsListRequestBuilder();
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

