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
/// * [outputCommaOmitempty] 
/// * [success] 
/// * [errorCommaOmitempty] 
@BuiltValue()
abstract class TemplatesInvokeResult implements Built<TemplatesInvokeResult, TemplatesInvokeResultBuilder> {
  @BuiltValueField(wireName: r'prompt')
  String get prompt;

  @BuiltValueField(wireName: r'output,omitempty')
  String? get outputCommaOmitempty;

  @BuiltValueField(wireName: r'success')
  bool get success;

  @BuiltValueField(wireName: r'error,omitempty')
  String? get errorCommaOmitempty;

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
    if (object.outputCommaOmitempty != null) {
      yield r'output,omitempty';
      yield serializers.serialize(
        object.outputCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'success';
    yield serializers.serialize(
      object.success,
      specifiedType: const FullType(bool),
    );
    if (object.errorCommaOmitempty != null) {
      yield r'error,omitempty';
      yield serializers.serialize(
        object.errorCommaOmitempty,
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
        case r'output,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.outputCommaOmitempty = valueDes;
          break;
        case r'success':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.success = valueDes;
          break;
        case r'error,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.errorCommaOmitempty = valueDes;
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

