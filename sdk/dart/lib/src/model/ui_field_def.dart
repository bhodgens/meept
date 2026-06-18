//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'ui_field_def.g.dart';

/// UIFieldDef
///
/// Properties:
/// * [name] 
/// * [label] 
/// * [type] 
/// * [requiredCommaOmitempty] 
/// * [defaultCommaOmitempty] 
/// * [optionsCommaOmitempty] 
/// * [placeholderCommaOmitempty] 
/// * [helpCommaOmitempty] 
@BuiltValue()
abstract class UIFieldDef implements Built<UIFieldDef, UIFieldDefBuilder> {
  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'label')
  String get label;

  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'required,omitempty')
  bool? get requiredCommaOmitempty;

  @BuiltValueField(wireName: r'default,omitempty')
  JsonObject? get defaultCommaOmitempty;

  @BuiltValueField(wireName: r'options,omitempty')
  String? get optionsCommaOmitempty;

  @BuiltValueField(wireName: r'placeholder,omitempty')
  String? get placeholderCommaOmitempty;

  @BuiltValueField(wireName: r'help,omitempty')
  String? get helpCommaOmitempty;

  UIFieldDef._();

  factory UIFieldDef([void updates(UIFieldDefBuilder b)]) = _$UIFieldDef;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(UIFieldDefBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<UIFieldDef> get serializer => _$UIFieldDefSerializer();
}

class _$UIFieldDefSerializer implements PrimitiveSerializer<UIFieldDef> {
  @override
  final Iterable<Type> types = const [UIFieldDef, _$UIFieldDef];

  @override
  final String wireName = r'UIFieldDef';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    UIFieldDef object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    yield r'label';
    yield serializers.serialize(
      object.label,
      specifiedType: const FullType(String),
    );
    yield r'type';
    yield serializers.serialize(
      object.type,
      specifiedType: const FullType(String),
    );
    if (object.requiredCommaOmitempty != null) {
      yield r'required,omitempty';
      yield serializers.serialize(
        object.requiredCommaOmitempty,
        specifiedType: const FullType(bool),
      );
    }
    if (object.defaultCommaOmitempty != null) {
      yield r'default,omitempty';
      yield serializers.serialize(
        object.defaultCommaOmitempty,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.optionsCommaOmitempty != null) {
      yield r'options,omitempty';
      yield serializers.serialize(
        object.optionsCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.placeholderCommaOmitempty != null) {
      yield r'placeholder,omitempty';
      yield serializers.serialize(
        object.placeholderCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.helpCommaOmitempty != null) {
      yield r'help,omitempty';
      yield serializers.serialize(
        object.helpCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    UIFieldDef object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required UIFieldDefBuilder result,
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
        case r'label':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.label = valueDes;
          break;
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.type = valueDes;
          break;
        case r'required,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.requiredCommaOmitempty = valueDes;
          break;
        case r'default,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.defaultCommaOmitempty = valueDes;
          break;
        case r'options,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.optionsCommaOmitempty = valueDes;
          break;
        case r'placeholder,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.placeholderCommaOmitempty = valueDes;
          break;
        case r'help,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.helpCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  UIFieldDef deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = UIFieldDefBuilder();
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

