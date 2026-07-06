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
/// * [required_] 
/// * [default_] 
/// * [options] 
/// * [placeholder] 
/// * [help] 
@BuiltValue()
abstract class UIFieldDef implements Built<UIFieldDef, UIFieldDefBuilder> {
  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'label')
  String get label;

  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'required')
  bool? get required_;

  @BuiltValueField(wireName: r'default')
  JsonObject? get default_;

  @BuiltValueField(wireName: r'options')
  String? get options;

  @BuiltValueField(wireName: r'placeholder')
  String? get placeholder;

  @BuiltValueField(wireName: r'help')
  String? get help;

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
    if (object.required_ != null) {
      yield r'required';
      yield serializers.serialize(
        object.required_,
        specifiedType: const FullType(bool),
      );
    }
    if (object.default_ != null) {
      yield r'default';
      yield serializers.serialize(
        object.default_,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.options != null) {
      yield r'options';
      yield serializers.serialize(
        object.options,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.placeholder != null) {
      yield r'placeholder';
      yield serializers.serialize(
        object.placeholder,
        specifiedType: const FullType(String),
      );
    }
    if (object.help != null) {
      yield r'help';
      yield serializers.serialize(
        object.help,
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
        case r'required':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.required_ = valueDes;
          break;
        case r'default':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.default_ = valueDes;
          break;
        case r'options':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.options = valueDes;
          break;
        case r'placeholder':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.placeholder = valueDes;
          break;
        case r'help':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.help = valueDes;
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

