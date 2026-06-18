//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_clear_result.g.dart';

/// TemplatesClearResult
///
/// Properties:
/// * [cleared] 
@BuiltValue()
abstract class TemplatesClearResult implements Built<TemplatesClearResult, TemplatesClearResultBuilder> {
  @BuiltValueField(wireName: r'cleared')
  String? get cleared;

  TemplatesClearResult._();

  factory TemplatesClearResult([void updates(TemplatesClearResultBuilder b)]) = _$TemplatesClearResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesClearResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesClearResult> get serializer => _$TemplatesClearResultSerializer();
}

class _$TemplatesClearResultSerializer implements PrimitiveSerializer<TemplatesClearResult> {
  @override
  final Iterable<Type> types = const [TemplatesClearResult, _$TemplatesClearResult];

  @override
  final String wireName = r'TemplatesClearResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesClearResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'cleared';
    yield object.cleared == null ? null : serializers.serialize(
      object.cleared,
      specifiedType: const FullType.nullable(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplatesClearResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesClearResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'cleared':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.cleared = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TemplatesClearResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesClearResultBuilder();
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

