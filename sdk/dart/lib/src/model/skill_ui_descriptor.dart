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
/// * [category] 
/// * [tags] 
/// * [examples] 
/// * [riskLevel] 
/// * [body] 
/// * [fields] 
/// * [actions] 
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

  @BuiltValueField(wireName: r'category')
  String? get category;

  @BuiltValueField(wireName: r'tags')
  String? get tags;

  @BuiltValueField(wireName: r'examples')
  String? get examples;

  @BuiltValueField(wireName: r'risk_level')
  String? get riskLevel;

  @BuiltValueField(wireName: r'body')
  String? get body;

  @BuiltValueField(wireName: r'fields')
  BuiltList<String>? get fields;

  @BuiltValueField(wireName: r'actions')
  BuiltList<String>? get actions;

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
    if (object.category != null) {
      yield r'category';
      yield serializers.serialize(
        object.category,
        specifiedType: const FullType(String),
      );
    }
    if (object.tags != null) {
      yield r'tags';
      yield serializers.serialize(
        object.tags,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.examples != null) {
      yield r'examples';
      yield serializers.serialize(
        object.examples,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.riskLevel != null) {
      yield r'risk_level';
      yield serializers.serialize(
        object.riskLevel,
        specifiedType: const FullType(String),
      );
    }
    if (object.body != null) {
      yield r'body';
      yield serializers.serialize(
        object.body,
        specifiedType: const FullType(String),
      );
    }
    if (object.fields != null) {
      yield r'fields';
      yield serializers.serialize(
        object.fields,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
    if (object.actions != null) {
      yield r'actions';
      yield serializers.serialize(
        object.actions,
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
        case r'category':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.category = valueDes;
          break;
        case r'tags':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.tags = valueDes;
          break;
        case r'examples':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.examples = valueDes;
          break;
        case r'risk_level':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.riskLevel = valueDes;
          break;
        case r'body':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.body = valueDes;
          break;
        case r'fields':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.fields.replace(valueDes);
          break;
        case r'actions':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.actions.replace(valueDes);
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

