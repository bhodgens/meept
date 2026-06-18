//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'ui_action_def.g.dart';

/// UIActionDef
///
/// Properties:
/// * [id] 
/// * [label] 
/// * [type] 
/// * [styleCommaOmitempty] 
@BuiltValue()
abstract class UIActionDef implements Built<UIActionDef, UIActionDefBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'label')
  String get label;

  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'style,omitempty')
  String? get styleCommaOmitempty;

  UIActionDef._();

  factory UIActionDef([void updates(UIActionDefBuilder b)]) = _$UIActionDef;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(UIActionDefBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<UIActionDef> get serializer => _$UIActionDefSerializer();
}

class _$UIActionDefSerializer implements PrimitiveSerializer<UIActionDef> {
  @override
  final Iterable<Type> types = const [UIActionDef, _$UIActionDef];

  @override
  final String wireName = r'UIActionDef';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    UIActionDef object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
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
    if (object.styleCommaOmitempty != null) {
      yield r'style,omitempty';
      yield serializers.serialize(
        object.styleCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    UIActionDef object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required UIActionDefBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
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
        case r'style,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.styleCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  UIActionDef deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = UIActionDefBuilder();
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

