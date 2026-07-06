//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_invoke_result.g.dart';

/// TemplatesInvokeResult
///
/// Properties:
/// * [prompt] 
/// * [output] 
/// * [success] 
/// * [error] 
@BuiltValue()
abstract class TemplatesInvokeResult implements Built<TemplatesInvokeResult, TemplatesInvokeResultBuilder> {
  @BuiltValueField(wireName: r'prompt')
  String get prompt;

  @BuiltValueField(wireName: r'output')
  String? get output;

  @BuiltValueField(wireName: r'success')
  bool get success;

  @BuiltValueField(wireName: r'error')
  String? get error;

  TemplatesInvokeResult._();

  factory TemplatesInvokeResult([void updates(TemplatesInvokeResultBuilder b)]) = _$TemplatesInvokeResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesInvokeResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesInvokeResult> get serializer => _$TemplatesInvokeResultSerializer();
}

class _$TemplatesInvokeResultSerializer implements PrimitiveSerializer<TemplatesInvokeResult> {
  @override
  final Iterable<Type> types = const [TemplatesInvokeResult, _$TemplatesInvokeResult];

  @override
  final String wireName = r'TemplatesInvokeResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesInvokeResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'prompt';
    yield serializers.serialize(
      object.prompt,
      specifiedType: const FullType(String),
    );
    if (object.output != null) {
      yield r'output';
      yield serializers.serialize(
        object.output,
        specifiedType: const FullType(String),
      );
    }
    yield r'success';
    yield serializers.serialize(
      object.success,
      specifiedType: const FullType(bool),
    );
    if (object.error != null) {
      yield r'error';
      yield serializers.serialize(
        object.error,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplatesInvokeResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesInvokeResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'prompt':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.prompt = valueDes;
          break;
        case r'output':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.output = valueDes;
          break;
        case r'success':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.success = valueDes;
          break;
        case r'error':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.error = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TemplatesInvokeResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesInvokeResultBuilder();
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

