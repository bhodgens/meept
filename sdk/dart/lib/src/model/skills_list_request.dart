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
/// * [categoryCommaOmitempty] 
/// * [limitCommaOmitempty] 
@BuiltValue()
abstract class SkillsListRequest implements Built<SkillsListRequest, SkillsListRequestBuilder> {
  @BuiltValueField(wireName: r'category,omitempty')
  String? get categoryCommaOmitempty;

  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

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
    if (object.categoryCommaOmitempty != null) {
      yield r'category,omitempty';
      yield serializers.serialize(
        object.categoryCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.limitCommaOmitempty != null) {
      yield r'limit,omitempty';
      yield serializers.serialize(
        object.limitCommaOmitempty,
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
        case r'category,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.categoryCommaOmitempty = valueDes;
          break;
        case r'limit,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limitCommaOmitempty = valueDes;
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

