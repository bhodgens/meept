//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'skill_info.g.dart';

/// SkillInfo
///
/// Properties:
/// * [slug] 
/// * [name] 
/// * [description] 
/// * [category] 
/// * [capabilities] 
/// * [enabled] 
/// * [uiType] 
@BuiltValue()
abstract class SkillInfo implements Built<SkillInfo, SkillInfoBuilder> {
  @BuiltValueField(wireName: r'slug')
  String get slug;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'description')
  String get description;

  @BuiltValueField(wireName: r'category')
  String? get category;

  @BuiltValueField(wireName: r'capabilities')
  String? get capabilities;

  @BuiltValueField(wireName: r'enabled')
  bool get enabled;

  @BuiltValueField(wireName: r'ui_type')
  String? get uiType;

  SkillInfo._();

  factory SkillInfo([void updates(SkillInfoBuilder b)]) = _$SkillInfo;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SkillInfoBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SkillInfo> get serializer => _$SkillInfoSerializer();
}

class _$SkillInfoSerializer implements PrimitiveSerializer<SkillInfo> {
  @override
  final Iterable<Type> types = const [SkillInfo, _$SkillInfo];

  @override
  final String wireName = r'SkillInfo';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SkillInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'slug';
    yield serializers.serialize(
      object.slug,
      specifiedType: const FullType(String),
    );
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    yield r'description';
    yield serializers.serialize(
      object.description,
      specifiedType: const FullType(String),
    );
    if (object.category != null) {
      yield r'category';
      yield serializers.serialize(
        object.category,
        specifiedType: const FullType(String),
      );
    }
    if (object.capabilities != null) {
      yield r'capabilities';
      yield serializers.serialize(
        object.capabilities,
        specifiedType: const FullType.nullable(String),
      );
    }
    yield r'enabled';
    yield serializers.serialize(
      object.enabled,
      specifiedType: const FullType(bool),
    );
    if (object.uiType != null) {
      yield r'ui_type';
      yield serializers.serialize(
        object.uiType,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SkillInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SkillInfoBuilder result,
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
        case r'name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.name = valueDes;
          break;
        case r'description':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.description = valueDes;
          break;
        case r'category':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.category = valueDes;
          break;
        case r'capabilities':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.capabilities = valueDes;
          break;
        case r'enabled':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.enabled = valueDes;
          break;
        case r'ui_type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.uiType = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SkillInfo deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SkillInfoBuilder();
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

