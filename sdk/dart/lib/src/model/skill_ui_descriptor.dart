//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'skill_ui_descriptor.g.dart';

/// SkillUIDescriptor
///
/// Properties:
/// * [slug] 
/// * [name] 
/// * [description] 
/// * [uiType] 
/// * [categoryCommaOmitempty] 
/// * [tagsCommaOmitempty] 
/// * [examplesCommaOmitempty] 
/// * [riskLevelCommaOmitempty] 
/// * [bodyCommaOmitempty] 
/// * [fieldsCommaOmitempty] 
/// * [actionsCommaOmitempty] 
@BuiltValue()
abstract class SkillUIDescriptor implements Built<SkillUIDescriptor, SkillUIDescriptorBuilder> {
  @BuiltValueField(wireName: r'slug')
  String get slug;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'description')
  String get description;

  @BuiltValueField(wireName: r'ui_type')
  String get uiType;

  @BuiltValueField(wireName: r'category,omitempty')
  String? get categoryCommaOmitempty;

  @BuiltValueField(wireName: r'tags,omitempty')
  String? get tagsCommaOmitempty;

  @BuiltValueField(wireName: r'examples,omitempty')
  String? get examplesCommaOmitempty;

  @BuiltValueField(wireName: r'risk_level,omitempty')
  String? get riskLevelCommaOmitempty;

  @BuiltValueField(wireName: r'body,omitempty')
  String? get bodyCommaOmitempty;

  @BuiltValueField(wireName: r'fields,omitempty')
  BuiltList<String>? get fieldsCommaOmitempty;

  @BuiltValueField(wireName: r'actions,omitempty')
  BuiltList<String>? get actionsCommaOmitempty;

  SkillUIDescriptor._();

  factory SkillUIDescriptor([void updates(SkillUIDescriptorBuilder b)]) = _$SkillUIDescriptor;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SkillUIDescriptorBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SkillUIDescriptor> get serializer => _$SkillUIDescriptorSerializer();
}

class _$SkillUIDescriptorSerializer implements PrimitiveSerializer<SkillUIDescriptor> {
  @override
  final Iterable<Type> types = const [SkillUIDescriptor, _$SkillUIDescriptor];

  @override
  final String wireName = r'SkillUIDescriptor';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SkillUIDescriptor object, {
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
    yield r'ui_type';
    yield serializers.serialize(
      object.uiType,
      specifiedType: const FullType(String),
    );
    if (object.categoryCommaOmitempty != null) {
      yield r'category,omitempty';
      yield serializers.serialize(
        object.categoryCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.tagsCommaOmitempty != null) {
      yield r'tags,omitempty';
      yield serializers.serialize(
        object.tagsCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.examplesCommaOmitempty != null) {
      yield r'examples,omitempty';
      yield serializers.serialize(
        object.examplesCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.riskLevelCommaOmitempty != null) {
      yield r'risk_level,omitempty';
      yield serializers.serialize(
        object.riskLevelCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.bodyCommaOmitempty != null) {
      yield r'body,omitempty';
      yield serializers.serialize(
        object.bodyCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.fieldsCommaOmitempty != null) {
      yield r'fields,omitempty';
      yield serializers.serialize(
        object.fieldsCommaOmitempty,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
    if (object.actionsCommaOmitempty != null) {
      yield r'actions,omitempty';
      yield serializers.serialize(
        object.actionsCommaOmitempty,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SkillUIDescriptor object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SkillUIDescriptorBuilder result,
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
        case r'ui_type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.uiType = valueDes;
          break;
        case r'category,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.categoryCommaOmitempty = valueDes;
          break;
        case r'tags,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.tagsCommaOmitempty = valueDes;
          break;
        case r'examples,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.examplesCommaOmitempty = valueDes;
          break;
        case r'risk_level,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.riskLevelCommaOmitempty = valueDes;
          break;
        case r'body,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.bodyCommaOmitempty = valueDes;
          break;
        case r'fields,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.fieldsCommaOmitempty.replace(valueDes);
          break;
        case r'actions,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.actionsCommaOmitempty.replace(valueDes);
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SkillUIDescriptor deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SkillUIDescriptorBuilder();
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

