//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_invoke_request.g.dart';

/// TemplatesInvokeRequest
///
/// Properties:
/// * [name] 
/// * [argsCommaOmitempty] 
@BuiltValue()
abstract class TemplatesInvokeRequest implements Built<TemplatesInvokeRequest, TemplatesInvokeRequestBuilder> {
  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'args,omitempty')
  String? get argsCommaOmitempty;

  TemplatesInvokeRequest._();

  factory TemplatesInvokeRequest([void updates(TemplatesInvokeRequestBuilder b)]) = _$TemplatesInvokeRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesInvokeRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesInvokeRequest> get serializer => _$TemplatesInvokeRequestSerializer();
}

class _$TemplatesInvokeRequestSerializer implements PrimitiveSerializer<TemplatesInvokeRequest> {
  @override
  final Iterable<Type> types = const [TemplatesInvokeRequest, _$TemplatesInvokeRequest];

  @override
  final String wireName = r'TemplatesInvokeRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesInvokeRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    if (object.argsCommaOmitempty != null) {
      yield r'args,omitempty';
      yield serializers.serialize(
        object.argsCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplatesInvokeRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesInvokeRequestBuilder result,
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
        case r'args,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.argsCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TemplatesInvokeRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesInvokeRequestBuilder();
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

