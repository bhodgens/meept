//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'security_service.g.dart';

/// SecurityService
///
/// Properties:
/// * [checker] 
@BuiltValue()
abstract class SecurityService implements Built<SecurityService, SecurityServiceBuilder> {
  @BuiltValueField(wireName: r'checker')
  JsonObject? get checker;

  SecurityService._();

  factory SecurityService([void updates(SecurityServiceBuilder b)]) = _$SecurityService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SecurityServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SecurityService> get serializer => _$SecurityServiceSerializer();
}

class _$SecurityServiceSerializer implements PrimitiveSerializer<SecurityService> {
  @override
  final Iterable<Type> types = const [SecurityService, _$SecurityService];

  @override
  final String wireName = r'SecurityService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SecurityService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.checker != null) {
      yield r'checker';
      yield serializers.serialize(
        object.checker,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SecurityService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SecurityServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'checker':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.checker = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SecurityService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SecurityServiceBuilder();
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

