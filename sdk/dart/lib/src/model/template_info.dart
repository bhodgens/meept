//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'template_info.g.dart';

/// TemplateInfo
///
/// Properties:
/// * [name] 
/// * [description] 
/// * [scope] 
/// * [pathCommaOmitempty] 
/// * [priority] 
/// * [bodyCommaOmitempty] 
@BuiltValue()
abstract class TemplateInfo implements Built<TemplateInfo, TemplateInfoBuilder> {
  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'description')
  String get description;

  @BuiltValueField(wireName: r'scope')
  JsonObject get scope;

  @BuiltValueField(wireName: r'path,omitempty')
  String? get pathCommaOmitempty;

  @BuiltValueField(wireName: r'priority')
  int get priority;

  @BuiltValueField(wireName: r'body,omitempty')
  String? get bodyCommaOmitempty;

  TemplateInfo._();

  factory TemplateInfo([void updates(TemplateInfoBuilder b)]) = _$TemplateInfo;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplateInfoBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplateInfo> get serializer => _$TemplateInfoSerializer();
}

class _$TemplateInfoSerializer implements PrimitiveSerializer<TemplateInfo> {
  @override
  final Iterable<Type> types = const [TemplateInfo, _$TemplateInfo];

  @override
  final String wireName = r'TemplateInfo';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplateInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
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
    yield r'scope';
    yield serializers.serialize(
      object.scope,
      specifiedType: const FullType(JsonObject),
    );
    if (object.pathCommaOmitempty != null) {
      yield r'path,omitempty';
      yield serializers.serialize(
        object.pathCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'priority';
    yield serializers.serialize(
      object.priority,
      specifiedType: const FullType(int),
    );
    if (object.bodyCommaOmitempty != null) {
      yield r'body,omitempty';
      yield serializers.serialize(
        object.bodyCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplateInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplateInfoBuilder result,
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
        case r'description':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.description = valueDes;
          break;
        case r'scope':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.scope = valueDes;
          break;
        case r'path,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.pathCommaOmitempty = valueDes;
          break;
        case r'priority':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.priority = valueDes;
          break;
        case r'body,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.bodyCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TemplateInfo deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplateInfoBuilder();
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

