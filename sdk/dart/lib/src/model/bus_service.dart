//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'bus_service.g.dart';

/// BusService
///
/// Properties:
/// * [bus] 
@BuiltValue()
abstract class BusService implements Built<BusService, BusServiceBuilder> {
  @BuiltValueField(wireName: r'bus')
  JsonObject? get bus;

  BusService._();

  factory BusService([void updates(BusServiceBuilder b)]) = _$BusService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(BusServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<BusService> get serializer => _$BusServiceSerializer();
}

class _$BusServiceSerializer implements PrimitiveSerializer<BusService> {
  @override
  final Iterable<Type> types = const [BusService, _$BusService];

  @override
  final String wireName = r'BusService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    BusService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.bus != null) {
      yield r'bus';
      yield serializers.serialize(
        object.bus,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    BusService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required BusServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'bus':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.bus = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  BusService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = BusServiceBuilder();
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

