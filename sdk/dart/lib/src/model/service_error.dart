//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'service_error.g.dart';

/// ServiceError
///
/// Properties:
/// * [service] 
/// * [op] 
/// * [err] 
@BuiltValue()
abstract class ServiceError implements Built<ServiceError, ServiceErrorBuilder> {
  @BuiltValueField(wireName: r'Service')
  String? get service;

  @BuiltValueField(wireName: r'Op')
  String? get op;

  @BuiltValueField(wireName: r'Err')
  JsonObject? get err;

  ServiceError._();

  factory ServiceError([void updates(ServiceErrorBuilder b)]) = _$ServiceError;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ServiceErrorBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ServiceError> get serializer => _$ServiceErrorSerializer();
}

class _$ServiceErrorSerializer implements PrimitiveSerializer<ServiceError> {
  @override
  final Iterable<Type> types = const [ServiceError, _$ServiceError];

  @override
  final String wireName = r'ServiceError';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ServiceError object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.service != null) {
      yield r'Service';
      yield serializers.serialize(
        object.service,
        specifiedType: const FullType(String),
      );
    }
    if (object.op != null) {
      yield r'Op';
      yield serializers.serialize(
        object.op,
        specifiedType: const FullType(String),
      );
    }
    if (object.err != null) {
      yield r'Err';
      yield serializers.serialize(
        object.err,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ServiceError object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ServiceErrorBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'Service':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.service = valueDes;
          break;
        case r'Op':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.op = valueDes;
          break;
        case r'Err':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.err = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ServiceError deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ServiceErrorBuilder();
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

